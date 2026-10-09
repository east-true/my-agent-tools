package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/google/go-github/v92/github"
)

const ciCompilerLog = "2026-10-07T00:00:00Z setup\n" +
	"2026-10-07T00:00:01Z src/build.go:7:2: too many arguments in call to pkg.Build\n" +
	"2026-10-07T00:00:01Z have (int, bool)\n" +
	"2026-10-07T00:00:01Z want (int)\n" +
	"2026-10-07T00:00:01Z src/build.go:9:4: T does not implement I (missing method Flush)\n" +
	"2026-10-07T00:00:02Z ##[error]Process completed with exit code 1.\n" +
	"2026-10-07T00:00:03Z cleanup\n"

type ciLogFixture struct {
	API
	logs      map[int64]string
	logError  error
	truncated bool
	downloads []int64
}

func (api *ciLogFixture) CIJobLog(_ context.Context, _ string, id, _ int64) (string, bool, error) {
	api.downloads = append(api.downloads, id)
	return api.logs[id], api.truncated, api.logError
}

func ciRunFixture(w http.ResponseWriter) {
	fmt.Fprint(w, `{"id":42,"run_attempt":2,"head_sha":"abc123","status":"completed","conclusion":"failure","name":"ci","html_url":"https://github.com/owner/repo/actions/runs/42"}`)
}

func TestCIFailuresPinsAttemptPaginatesAndDeduplicatesMatrixFacts(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/actions/runs/42":
			ciRunFixture(w)
		case "/repos/owner/repo/actions/runs/42/attempts/2/jobs":
			if r.URL.Query().Get("per_page") != "100" {
				t.Error("missing page size")
			}
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/actions/runs/42/attempts/2/jobs?page=2>; rel="next"`, r.Host))
				fmt.Fprint(w, `{"jobs":[{"id":10,"name":"lint","status":"completed","conclusion":"success"},{"id":11,"name":"test (linux)","status":"completed","conclusion":"failure","html_url":"https://example.com/jobs/11","check_run_url":"https://api.github.com/repos/owner/repo/check-runs/91","steps":[{"number":2,"name":"build","conclusion":"failure","started_at":"2026-10-07T00:00:01Z","completed_at":"2026-10-07T00:00:02Z"}]}]}`)
			} else {
				fmt.Fprint(w, `{"jobs":[{"id":12,"name":"test (windows)","status":"completed","conclusion":"failure","html_url":"https://example.com/jobs/12","steps":[{"number":2,"name":"build","conclusion":"failure","started_at":"2026-10-07T00:00:01Z","completed_at":"2026-10-07T00:00:02Z"}]}]}`)
			}
		case "/repos/owner/repo/check-runs/91/annotations":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/check-runs/91/annotations?page=2>; rel="next"`, r.Host))
				fmt.Fprint(w, `[{"path":"src/build.go","start_line":7,"end_line":7,"annotation_level":"failure","message":"compile error"}]`)
			} else {
				fmt.Fprint(w, `[{"annotation_level":"notice","message":"ignore"},{"path":"src/build.go","start_line":9,"end_line":9,"annotation_level":"warning","message":"warning"}]`)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(500)
		}
	})
	api := &ciLogFixture{API: f.client.API, logs: map[int64]string{11: ciCompilerLog, 12: ciCompilerLog}}
	f.client.API = api
	result, err := f.client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: true})
	if err != nil || !result.Complete || result.Status != "ok" || len(result.Jobs) != 2 || len(f.writes()) != 0 || !reflect.DeepEqual(api.downloads, []int64{11, 12}) {
		t.Fatalf("result=%+v err=%v downloads=%v", result, err, api.downloads)
	}
	if len(result.Jobs[0].Annotations) != 2 || len(result.Evidence) != 1 {
		t.Fatalf("missing annotations or deduplication: %+v", result)
	}
	evidence := result.Evidence[0]
	if evidence.Kind != "go_compiler" || len(evidence.Diagnostics) != 2 || evidence.ExpectedArguments["pkg.Build"] != 1 || !reflect.DeepEqual(evidence.MissingMethods, []string{"Flush"}) || len(evidence.Occurrences) != 2 || len(evidence.Lines) != 0 {
		t.Fatalf("bad compiler facts: %+v", evidence)
	}
	if evidence.Occurrences[0].StartLine != 2 || evidence.Occurrences[0].EndLine != 6 || evidence.Occurrences[0].StepNumber != 2 {
		t.Fatalf("wrong original line references: %+v", evidence.Occurrences)
	}
}

func TestCIFailuresUnknownFailureAndPartialCollection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		log        string
		logErr     error
		truncated  bool
		annotation bool
		complete   bool
	}{
		{name: "unknown", log: "expected: 10\nactual: 20\nnetwork disconnected\n", complete: true},
		{name: "truncated", log: ciCompilerLog, truncated: true},
		{name: "empty"},
		{name: "expired", logErr: errors.New("HTTP 410")},
		{name: "annotation forbidden", log: ciCompilerLog, annotation: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo/actions/runs/42":
					ciRunFixture(w)
				case "/repos/owner/repo/actions/runs/42/attempts/2/jobs":
					fmt.Fprint(w, `{"jobs":[{"id":11,"status":"completed","conclusion":"failure","check_run_url":"https://api.github.com/repos/owner/repo/check-runs/91"}]}`)
				default:
					w.WriteHeader(403)
					fmt.Fprint(w, `{"message":"Forbidden"}`)
				}
			})
			api := &ciLogFixture{API: f.client.API, logs: map[int64]string{11: tc.log}, logError: tc.logErr, truncated: tc.truncated}
			f.client.API = api
			result, err := f.client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: tc.annotation})
			if err != nil || result.Complete != tc.complete || len(result.Jobs) != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if !tc.complete && (result.Status != "partial" || len(result.Jobs[0].Notes) == 0) {
				t.Fatalf("partial data presented as success: %+v", result)
			}
			if tc.name == "unknown" && (len(result.Evidence) != 1 || result.Evidence[0].Kind != "log" || strings.Join(result.Evidence[0].Lines, "\n")+"\n" != tc.log) {
				t.Fatalf("unknown evidence lost: %+v", result.Evidence)
			}
			if tc.truncated && (len(result.Evidence) != 1 || result.Evidence[0].Kind != "log" || !result.Evidence[0].Truncated) {
				t.Fatalf("truncated data compacted as complete facts: %+v", result.Evidence)
			}
		})
	}
}

func TestCIFailuresRunStatesAndRepeatedPages(t *testing.T) {
	for _, state := range []string{"success", "running", "no failed jobs", "repeated page", "forbidden"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/actions/runs/42" {
					if state == "forbidden" {
						w.WriteHeader(403)
						fmt.Fprint(w, `{"message":"Forbidden"}`)
					} else if state == "success" {
						fmt.Fprint(w, `{"id":42,"run_attempt":2,"head_sha":"abc","status":"completed","conclusion":"success"}`)
					} else if state == "running" {
						fmt.Fprint(w, `{"id":42,"run_attempt":2,"head_sha":"abc","status":"in_progress"}`)
					} else {
						ciRunFixture(w)
					}
					return
				}
				if state == "repeated page" {
					w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/actions/runs/42/attempts/2/jobs?page=1>; rel="next"`, r.Host))
				}
				fmt.Fprint(w, `{"jobs":[]}`)
			})
			result, err := f.client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42})
			if state == "forbidden" || state == "repeated page" {
				if err == nil {
					t.Fatal("API/pagination failure accepted")
				}
			} else if err != nil || (result.Complete != (state == "success")) || len(result.Jobs) != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestCILogDownloadUsesNoAuthorizationAtSignedURL(t *testing.T) {
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/logs") {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("API request missing auth")
			}
			w.Header().Set("Location", base+"signed?signature=example")
			w.WriteHeader(302)
		} else {
			if r.Header.Get("Authorization") != "" {
				t.Error("GitHub token forwarded to signed storage URL")
			}
			fmt.Fprint(w, "0123456789")
		}
	}))
	defer server.Close()
	base = server.URL + "/"
	client, err := sdk.NewClient(sdk.WithAuthToken("test-token"), sdk.WithHTTPClient(server.Client()), sdk.WithURLs(&base, nil))
	if err != nil {
		t.Fatal(err)
	}
	api := SDK{Client: client}
	log, truncated, err := api.CIJobLog(context.Background(), "owner/repo", 11, 4)
	if err != nil || log != "0123" || !truncated {
		t.Fatalf("log=%q truncated=%t err=%v", log, truncated, err)
	}
	log, truncated, err = api.CIJobLog(context.Background(), "owner/repo", 11, 10)
	if err != nil || log != "0123456789" || truncated {
		t.Fatalf("exact limit: log=%q truncated=%t err=%v", log, truncated, err)
	}
}

func TestCIFailuresPreservesCollectedPagesWhenLaterPagesFail(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/actions/runs/42" {
			ciRunFixture(w)
			return
		}
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(500)
			fmt.Fprint(w, `{"message":"later page unavailable"}`)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
		if strings.Contains(r.URL.Path, "/annotations") {
			fmt.Fprint(w, `[{"path":"src/build.go","start_line":7,"end_line":7,"annotation_level":"failure","message":"compile error"}]`)
		} else {
			fmt.Fprint(w, `{"jobs":[{"id":11,"status":"completed","conclusion":"failure","check_run_url":"https://api.github.com/repos/owner/repo/check-runs/91"}]}`)
		}
	})
	f.client.API = &ciLogFixture{API: f.client.API, logs: map[int64]string{11: ciCompilerLog}}
	result, err := f.client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: true})
	if err != nil || result.Complete || result.Status != "partial" || len(result.Jobs) != 1 || len(result.Jobs[0].Annotations) != 1 || len(result.Evidence) != 1 || len(result.Notes) == 0 || len(result.Jobs[0].Notes) == 0 {
		t.Fatalf("collected pages lost: %+v err=%v", result, err)
	}
}
