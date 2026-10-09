package github

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type restRepresentation struct {
	Header http.Header
	Body   []byte
}
type readTransport struct {
	base      http.RoundTripper
	cache     cacheConfig
	wait      func(context.Context, time.Duration) error
	mu        sync.Mutex
	pollAfter map[string]time.Time
}

func waitRead(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type timedBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (body timedBody) Close() error { err := body.ReadCloser.Close(); body.cancel(); return err }

type joinedBody struct {
	io.Reader
	io.Closer
}

func (transport *readTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}
	if request.Method != http.MethodGet {
		ctx, cancel := context.WithTimeout(request.Context(), 60*time.Second)
		response, err := base.RoundTrip(request.Clone(ctx))
		if err != nil {
			cancel()
			return nil, err
		}
		response.Body = timedBody{response.Body, cancel}
		return response, nil
	}
	wait := transport.wait
	if wait == nil {
		wait = waitRead
	}
	key := request.URL.String() + "\n" + request.Header.Get("Accept") + "\n" + request.Header.Get("X-GitHub-Api-Version")
	config := transport.cache
	// Scope requests by the credential actually placed on the wire. Nothing
	// derived from Authorization is serialized except a one-way fingerprint.
	if auth := request.Header.Get("Authorization"); auth != "" {
		config.Scope = cacheHash([]byte(auth))
	} else {
		config.Root = ""
	}
	var cached restRepresentation
	hasCached := config.load("http", key, time.Hour, &cached)
	transport.mu.Lock()
	next := transport.pollAfter[config.Scope+key]
	transport.mu.Unlock()
	if next.After(time.Now()) {
		if err := wait(request.Context(), time.Until(next)); err != nil {
			return nil, err
		}
	}
	for attempt := 0; ; attempt++ {
		ctx, cancel := context.WithTimeout(request.Context(), 60*time.Second)
		req := request.Clone(ctx)
		if hasCached && req.Header.Get("If-None-Match") == "" && req.Header.Get("If-Modified-Since") == "" {
			if tag := cached.Header.Get("ETag"); tag != "" {
				req.Header.Set("If-None-Match", tag)
			} else if stamp := cached.Header.Get("Last-Modified"); stamp != "" {
				req.Header.Set("If-Modified-Since", stamp)
			}
		}
		ownedCondition := hasCached && ((cached.Header.Get("ETag") != "" && req.Header.Get("If-None-Match") == cached.Header.Get("ETag")) || (req.Header.Get("If-None-Match") == "" && cached.Header.Get("Last-Modified") != "" && req.Header.Get("If-Modified-Since") == cached.Header.Get("Last-Modified")))
		response, err := base.RoundTrip(req)
		if err != nil {
			cancel()
			return nil, err
		}
		response.Body = timedBody{response.Body, cancel}
		limited := response.StatusCode == 429 || (response.StatusCode == 403 && (response.Header.Get("Retry-After") != "" || response.Header.Get("X-RateLimit-Remaining") == "0"))
		if response.StatusCode == 403 && !limited {
			prefix, readErr := io.ReadAll(io.LimitReader(response.Body, 16<<10))
			if readErr != nil {
				response.Body.Close()
				return nil, readErr
			}
			body := response.Body
			response.Body = joinedBody{io.MultiReader(bytes.NewReader(prefix), body), body}
			message := strings.ToLower(string(prefix))
			limited = strings.Contains(message, "rate limit") || strings.Contains(message, "abuse detection")
		}
		retry := limited || response.StatusCode == 500 || response.StatusCode == 502 || response.StatusCode == 503 || response.StatusCode == 504
		if retry && attempt < 2 {
			delay := readRetryDelay(response.Header, limited, attempt, time.Now())
			response.Body.Close()
			if err := wait(request.Context(), delay); err != nil {
				return nil, err
			}
			continue
		}
		if seconds, err := strconv.Atoi(response.Header.Get("X-Poll-Interval")); err == nil && seconds > 0 && seconds <= 86400 {
			transport.mu.Lock()
			if transport.pollAfter == nil {
				transport.pollAfter = map[string]time.Time{}
			}
			transport.pollAfter[config.Scope+key] = time.Now().Add(time.Duration(seconds) * time.Second)
			transport.mu.Unlock()
		}
		if response.StatusCode == http.StatusNotModified && ownedCondition {
			response.Body.Close()
			header := cached.Header.Clone()
			for name, values := range response.Header {
				header[name] = values
			}
			response.StatusCode, response.Status = http.StatusOK, "200 OK"
			response.Header, response.Body = header, io.NopCloser(bytes.NewReader(cached.Body))
			response.ContentLength = int64(len(cached.Body))
			response.Header.Del("Content-Encoding")
			response.Header.Set("Content-Length", strconv.Itoa(len(cached.Body)))
			config.save("http", key, time.Hour, restRepresentation{cacheResponseHeaders(header), cached.Body})
			return response, nil
		}
		if config.Root != "" && response.StatusCode == 200 && (response.Header.Get("ETag") != "" || response.Header.Get("Last-Modified") != "") && response.ContentLength <= 16<<20 && !strings.Contains(strings.ToLower(response.Header.Get("Cache-Control")), "no-store") {
			body := response.Body
			data, err := io.ReadAll(io.LimitReader(body, (16<<20)+1))
			if err != nil {
				body.Close()
				return nil, err
			}
			if len(data) > 16<<20 {
				response.Body = joinedBody{io.MultiReader(bytes.NewReader(data), body), body}
				return response, nil
			}
			body.Close()
			response.Body = io.NopCloser(bytes.NewReader(data))
			config.save("http", key, time.Hour, restRepresentation{cacheResponseHeaders(response.Header), data})
		}
		return response, nil
	}
}

func cacheResponseHeaders(header http.Header) http.Header {
	result := http.Header{}
	for _, name := range []string{"Content-Type", "ETag", "Last-Modified", "Link"} {
		if values := header.Values(name); len(values) > 0 {
			result[http.CanonicalHeaderKey(name)] = append([]string{}, values...)
		}
	}
	return result
}

func readRetryDelay(header http.Header, limited bool, attempt int, now time.Time) time.Duration {
	if value := header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
		if stamp, err := http.ParseTime(value); err == nil {
			return max(0, stamp.Sub(now))
		}
	}
	if header.Get("X-RateLimit-Remaining") == "0" {
		if seconds, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil && seconds > 0 {
			remaining := time.Unix(seconds, 0).Sub(now)
			if remaining > 0 && remaining < time.Duration(1<<63-1)-time.Second {
				return remaining + time.Second
			}
			return max(time.Second, remaining)
		}
	}
	if limited {
		return time.Minute * time.Duration(1<<attempt)
	}
	return time.Second * time.Duration(1<<attempt)
}

var _ http.RoundTripper = (*readTransport)(nil)
