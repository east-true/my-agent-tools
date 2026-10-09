package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type readRoundTripFunc func(*http.Request) (*http.Response, error)

func (call readRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return call(req) }

func TestConditionalGETReusesAuthorizedRepresentationAndPagination(t *testing.T) {
	reads, conditionals := 0, 0
	denied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		if denied {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"message":"denied"}`)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			conditionals++
			w.WriteHeader(304)
			return
		}
		w.Header().Set("Link", `<https://api.github.com/items?page=2>; rel="next"`)
		w.Header().Set("Set-Cookie", "do-not-save-this-cookie")
		fmt.Fprint(w, `{"value":42}`)
	}))
	defer server.Close()
	root := t.TempDir()
	do := func(token string) (*http.Response, string) {
		client := &http.Client{Transport: &readTransport{cache: cacheConfig{Root: root}}}
		req, _ := http.NewRequest("GET", server.URL+"/items?page=1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		return response, string(body)
	}
	_, first := do("test-credential-a")
	response, second := do("test-credential-a")
	if reads != 2 || conditionals != 1 || response.StatusCode != 200 || first != second || response.Header.Get("Link") == "" {
		t.Fatalf("lost cached body/links: reads=%d conditional=%d response=%+v", reads, conditionals, response)
	}
	_, _ = do("test-credential-b")
	if conditionals != 1 {
		t.Fatal("another credential reused validators")
	}
	denied = true
	response, body := do("test-credential-a")
	if response.StatusCode != 403 || !strings.Contains(body, "denied") {
		t.Fatal("cached body bypassed authorization failure")
	}
	files, _ := filepath.Glob(filepath.Join(root, "*", "http", "*.json"))
	for _, file := range files {
		data, _ := os.ReadFile(file)
		if strings.Contains(string(data), "test-credential") || strings.Contains(string(data), "do-not-save-this-cookie") {
			t.Fatal("credential or cookie stored in HTTP cache")
		}
	}
}

func TestReadRetriesRespectRateLimitsAndNeverRetryMutations(t *testing.T) {
	for _, mode := range []string{"retry-after", "secondary limit", "reset", "transient", "forbidden", "exhausted", "cancelled", "POST", "PUT", "PATCH", "DELETE"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			delays := []time.Duration{}
			transport := &readTransport{wait: func(ctx context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				if mode == "cancelled" {
					return context.Canceled
				}
				return nil
			}}
			transport.base = readRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				status, body := 200, `{"ok":true}`
				header := http.Header{}
				if calls == 1 || mode == "exhausted" {
					switch mode {
					case "retry-after", "cancelled", "POST", "PUT", "PATCH", "DELETE":
						status = 429
						header.Set("Retry-After", "2")
					case "secondary limit":
						status = 403
						body = `{"message":"secondary rate limit"}`
					case "reset":
						status = 403
						header.Set("X-RateLimit-Remaining", "0")
						header.Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(3*time.Second).Unix()))
					case "forbidden":
						status = 403
						body = `{"message":"permission denied"}`
					case "transient", "exhausted":
						status = 503
					}
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			method := "GET"
			if mode == "POST" || mode == "PUT" || mode == "PATCH" || mode == "DELETE" {
				method = mode
			}
			req, _ := http.NewRequest(method, "https://api.github.com/resource", nil)
			response, err := transport.RoundTrip(req)
			if mode == "cancelled" {
				if !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatal("cancelled read retried")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if mode == "forbidden" || method != "GET" {
				if calls != 1 || len(delays) != 0 {
					t.Fatal("permission error or mutation retried")
				}
				return
			}
			if mode == "exhausted" {
				if calls != 3 || len(delays) != 2 || response.StatusCode != 503 {
					t.Fatal("read retry limit ignored")
				}
				return
			}
			if calls != 2 || len(delays) != 1 {
				t.Fatalf("calls=%d delays=%v", calls, delays)
			}
			if mode == "retry-after" && delays[0] != 2*time.Second || mode == "secondary limit" && delays[0] != time.Minute || mode == "reset" && (delays[0] < 2*time.Second || delays[0] > 5*time.Second) {
				t.Fatalf("wrong retry delay: %v", delays)
			}
		})
	}
}

func TestPollIntervalAndContextAreHonored(t *testing.T) {
	delays := []time.Duration{}
	transport := &readTransport{wait: func(ctx context.Context, d time.Duration) error { delays = append(delays, d); return nil }, base: readRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Poll-Interval": []string{"5"}}, Body: http.NoBody, Request: req}, nil
	})}
	req, _ := http.NewRequest("GET", "https://api.github.com/run", nil)
	for range 2 {
		response, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	if len(delays) != 1 || delays[0] < 4*time.Second || delays[0] > 5*time.Second {
		t.Fatalf("poll interval=%v", delays)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(waitRead(ctx, time.Minute), context.Canceled) {
		t.Fatal("wait ignored cancellation")
	}
}
