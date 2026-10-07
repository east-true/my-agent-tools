package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sdk "github.com/google/go-github/v92/github"
)

type noCommands struct{}

func (noCommands) Run(context.Context, []byte, string, ...string) ([]byte, error) {
	return nil, errors.New("unexpected external command")
}

type request struct {
	method string
	path   string
	body   map[string]any
}

type fixture struct {
	mu       sync.Mutex
	requests []request
	client   Client
}

func newFixture(t *testing.T, handler http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != http.MethodGet {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("invalid request body: %v", err)
			}
			encoded, _ := json.Marshal(body)
			r.Body = io.NopCloser(bytes.NewReader(encoded))
		}
		f.mu.Lock()
		f.requests = append(f.requests, request{r.Method, r.URL.RequestURI(), body})
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing SDK authentication header")
		}
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	baseURL := server.URL + "/"
	client, err := sdk.NewClient(sdk.WithAuthToken("test-token"), sdk.WithHTTPClient(server.Client()), sdk.WithURLs(&baseURL, nil))
	if err != nil {
		t.Fatal(err)
	}
	f.client = Client{Runner: noCommands{}, API: SDK{Client: client}}
	return f
}

func (f *fixture) writes() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var writes []request
	for _, req := range f.requests {
		if req.path == "/graphql" {
			if query, _ := req.body["query"].(string); strings.HasPrefix(query, "query") {
				continue
			}
		}
		if req.method != "GET" {
			writes = append(writes, req)
		}
	}
	return writes
}

func commonResponse(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/graphql":
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return false
		}
		if !strings.HasPrefix(payload.Query, "query RepositoryTemplates") {
			return false
		}
		fmt.Fprint(w, `{"data":{"repository":{"templates":[]}}}`)
	case "/repos/owner/repo":
		fmt.Fprint(w, `{"full_name":"owner/repo","default_branch":"main","permissions":{"push":true}}`)
	case "/repos/owner/repo/labels":
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/labels?per_page=100&page=2>; rel="next"`, r.Host))
			fmt.Fprint(w, `[{"name":"bug"}]`)
		} else {
			fmt.Fprint(w, `[{"name":"enhancement"}]`)
		}
	case "/repos/owner/repo/issue-types":
		fmt.Fprint(w, `[{"name":"Feature"},{"name":"Bug"},{"name":"Task"}]`)
	case "/user":
		fmt.Fprint(w, `{"login":"tester"}`)
	default:
		return false
	}
	return true
}

func TestIssuePlanAndCreateThroughSDK(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if commonResponse(w, r) {
			return
		}
		if r.Method != "POST" || r.URL.Path != "/repos/owner/repo/issues" {
			t.Errorf("unexpected API call: %s %s", r.Method, r.URL)
			w.WriteHeader(500)
			return
		}
		fmt.Fprint(w, `{"number":7,"html_url":"https://github.com/owner/repo/issues/7","labels":[{"name":"enhancement"}],"assignees":[{"login":"tester"}],"type":{"name":"Feature"}}`)
	})
	spec := Spec{Title: "feat: add cli", Body: "내용\n\n`literal` $(touch should-not-exist)\n따옴표: \"그대로\""}
	plan, err := f.client.Prepare(context.Background(), "owner/repo", "issue", spec, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.writes()) != 0 || plan.Payload["type"] != "Feature" || len(plan.Labels) != 1 || plan.Labels[0] != "enhancement" {
		t.Fatalf("bad plan or premature write: %+v", plan)
	}
	result, err := f.client.Create(context.Background(), plan)
	if err != nil || result.Status != "created" || result.Branch != "7-feat-add-cli" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	writes := f.writes()
	if len(writes) != 1 || writes[0].body["body"] != spec.Body {
		t.Fatalf("body changed or extra mutations: %+v", writes)
	}
}

func TestPRFailurePreservesCreatedURLAndDoesNotRetry(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if commonResponse(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/owner/repo/issues/7":
			fmt.Fprint(w, `{"number":7}`)
		case "/repos/owner/repo/compare/main...7-feat-add-cli":
			fmt.Fprint(w, `{"ahead_by":2}`)
		case "/repos/owner/repo/pulls":
			fmt.Fprint(w, `{"number":8,"html_url":"https://github.com/owner/repo/pull/8"}`)
		case "/repos/owner/repo/issues/8/labels":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"Resource not accessible"}`)
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL)
		}
	})
	plan, err := f.client.Prepare(context.Background(), "owner/repo", "pr", Spec{Title: "feat: add cli", Summary: "기능 추가", Head: "7-feat-add-cli", Issue: 7, Draft: true}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Payload["body"].(string), "Closes #7") || plan.Payload["draft"] != true {
		t.Fatalf("bad PR plan: %+v", plan)
	}
	result, err := f.client.Create(context.Background(), plan)
	if err == nil || result.Status != "partial" || result.URL != "https://github.com/owner/repo/pull/8" || len(f.writes()) != 2 {
		t.Fatalf("result=%+v err=%v writes=%v", result, err, f.writes())
	}
}

func TestIssueMetadataOmissionIsPartial(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if !commonResponse(w, r) {
			fmt.Fprint(w, `{"number":7,"html_url":"https://github.com/owner/repo/issues/7"}`)
		}
	})
	plan, err := f.client.Prepare(context.Background(), "owner/repo", "issue", Spec{Title: "feat: add cli", Body: "내용"}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.client.Create(context.Background(), plan)
	if err == nil || result.Status != "partial" || len(result.Notes) != 3 || result.Number != 7 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPRCreationWithSlashedHeadAndLabels(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if commonResponse(w, r) {
			return
		}
		switch r.URL.Path {
		case "/repos/owner/repo/compare/main...feat/add-cli":
			fmt.Fprint(w, `{"ahead_by":1}`)
		case "/repos/owner/repo/pulls":
			fmt.Fprint(w, `{"number":8,"html_url":"https://github.com/owner/repo/pull/8"}`)
		case "/repos/owner/repo/issues/8/labels":
			fmt.Fprint(w, `[{"name":"enhancement"}]`)
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL)
		}
	})
	plan, err := f.client.Prepare(context.Background(), "owner/repo", "pr", Spec{Title: "feat: add cli", Body: "변경과 검증 내용", Head: "feat/add-cli"}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.client.Create(context.Background(), plan)
	if err != nil || result.Status != "created" || len(f.writes()) != 2 {
		t.Fatalf("result=%+v err=%v writes=%v", result, err, f.writes())
	}
	if f.writes()[0].body["head"] != "feat/add-cli" || f.writes()[0].body["base"] != "main" {
		t.Fatalf("bad PR request: %+v", f.writes()[0])
	}
}

func TestIssueTypeUnavailableVersusForbidden(t *testing.T) {
	for _, status := range []int{404, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/issue-types" {
					w.WriteHeader(status)
					fmt.Fprint(w, `{"message":"unavailable"}`)
					return
				}
				commonResponse(w, r)
			})
			catalog, err := f.client.Context(context.Background(), "owner/repo")
			if status == 404 && (err != nil || len(catalog.Notes) == 0) {
				t.Fatalf("404 should be reported as unavailable: %+v %v", catalog, err)
			}
			if status == 403 && err == nil {
				t.Fatal("403 was silently ignored")
			}
		})
	}
}

func TestPRPreflightRejectsInvalidBranchAndUnpushedChanges(t *testing.T) {
	for _, head := range []string{"main", "feat/add-cli"} {
		t.Run(head, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if !commonResponse(w, r) {
					fmt.Fprint(w, `{"ahead_by":0}`)
				}
			})
			_, err := f.client.Prepare(context.Background(), "owner/repo", "pr", Spec{Title: "feat: add cli", Body: "내용", Head: head}, DefaultPolicy())
			if err == nil || len(f.writes()) != 0 {
				t.Fatalf("preflight accepted %s or mutated GitHub: %v", head, err)
			}
		})
	}
}

func TestEnvironmentTokenNeedsNoExternalCommand(t *testing.T) {
	t.Setenv("GH_TOKEN", "test-token")
	t.Setenv("GITHUB_TOKEN", "other-token")
	api, err := NewAPI(context.Background(), noCommands{})
	if err != nil || api == nil {
		t.Fatalf("environment token should not require gh: %v", err)
	}
}
