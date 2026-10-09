package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/google/go-github/v92/github"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type submitTestRunner struct {
	sha, branch, remote string
	dirty, pushError    bool
	pushes              [][]string
}

func (runner *submitTestRunner) Run(_ context.Context, _ []byte, name string, args ...string) ([]byte, error) {
	if name != "git" {
		return nil, errors.New("unexpected command")
	}
	switch args[0] {
	case "remote":
		return []byte(runner.remote), nil
	case "symbolic-ref":
		return []byte(runner.branch), nil
	case "status":
		if runner.dirty {
			return []byte(" M source.go"), nil
		}
		return nil, nil
	case "rev-parse":
		return []byte(runner.sha), nil
	case "push":
		runner.pushes = append(runner.pushes, append([]string{}, args...))
		if runner.pushError {
			return nil, errors.New("connection lost")
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected git arguments %v", args)
}

func TestSubmitPushesPinnedCommitAndCreatesOrReusesPR(t *testing.T) {
	for _, mode := range []string{"create", "reuse", "wait", "dry run", "dirty", "push error", "foreign remote", "remote changed", "invalid branch"} {
		t.Run(mode, func(t *testing.T) {
			runner := &submitTestRunner{sha: strings.Repeat("a", 40), branch: "fix/workflows", remote: "https://github.com/owner/repo.git"}
			runner.dirty, runner.pushError = mode == "dirty", mode == "push error"
			if mode == "foreign remote" {
				runner.remote = "https://github.com/other/repo.git"
			}
			if mode == "invalid branch" {
				runner.branch = "main"
			}
			creates := 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/owner/repo/git/ref/heads/main":
					fmt.Fprint(w, `{"ref":"refs/heads/main"}`)
				case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/ref/heads/"):
					sha := runner.sha
					if mode == "remote changed" {
						sha = strings.Repeat("b", 40)
					}
					fmt.Fprintf(w, `{"object":{"sha":%q}}`, sha)
				case r.URL.Path == "/repos/owner/repo/pulls" && r.Method == "GET":
					if mode == "reuse" {
						fmt.Fprintf(w, `[{"number":7,"state":"open","html_url":"https://github.com/owner/repo/pull/7","base":{"ref":"main"},"head":{"ref":"fix/workflows","sha":%q,"repo":{"full_name":"owner/repo"}}}]`, runner.sha)
					} else {
						fmt.Fprint(w, `[]`)
					}
				case r.URL.Path == "/repos/owner/repo/pulls" && r.Method == "POST":
					creates++
					var payload map[string]any
					_ = json.NewDecoder(r.Body).Decode(&payload)
					if payload["title"] != "fix: improve workflows" || payload["head"] != runner.branch || payload["body"] != "조합 명령 개선" {
						t.Errorf("payload=%+v", payload)
					}
					fmt.Fprint(w, `{"number":7,"html_url":"https://github.com/owner/repo/pull/7"}`)
				case r.URL.Path == "/repos/owner/repo/issues/7/labels":
					fmt.Fprint(w, `[{"name":"bug"}]`)
				case strings.Contains(r.URL.Path, "/compare/"):
					fmt.Fprint(w, `{"ahead_by":1}`)
				case r.URL.Path == "/graphql":
					var payload map[string]any
					_ = json.NewDecoder(r.Body).Decode(&payload)
					query := payload["query"].(string)
					switch {
					case strings.HasPrefix(query, "query RepositoryTemplates"):
						fmt.Fprint(w, `{"data":{"repository":{"templates":[]}}}`)
					case strings.HasPrefix(query, "query PullRequestReviews"):
						workflowReviewFixture(w, runner.sha, "APPROVED", []graphReviewThread{})
					default:
						mergeFixturePR(w, map[string]any{"headRefOid": runner.sha})
					}
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					mergeFixtureChecks(w)
				case strings.HasSuffix(r.URL.Path, "/statuses") || strings.HasSuffix(r.URL.Path, "/reviews"):
					fmt.Fprint(w, `[]`)
				case r.URL.Path == "/repos/owner/repo/pulls/7":
					fmt.Fprintf(w, `{"head":{"sha":%q}}`, runner.sha)
				default:
					if !commonResponse(w, r) {
						t.Errorf("unexpected request %s %s", r.Method, r.URL)
						w.WriteHeader(500)
					}
				}
			})
			f.client.Runner = runner
			options := SubmitOptions{Spec: Spec{Prefix: "fix", Title: "improve workflows", Body: "조합 명령 개선"}, Policy: DefaultPolicy(), Wait: mode == "wait", DryRun: mode == "dry run", Inspect: InspectOptions{Timeout: 2 * time.Second, Interval: time.Millisecond}}
			result, err := f.client.SubmitPR(context.Background(), "owner/repo", options)
			want, wantPushes, wantCreates := "submitted", 1, 1
			switch mode {
			case "reuse":
				wantCreates = 0
			case "dry run":
				want, wantPushes, wantCreates = "planned", 0, 0
			case "dirty", "foreign remote", "invalid branch":
				want, wantPushes, wantCreates = "error", 0, 0
			case "push error":
				want, wantCreates = "unknown", 0
			case "remote changed":
				want, wantCreates = "partial", 0
			}
			if result.Status != want || len(runner.pushes) != wantPushes || creates != wantCreates || (err != nil) != (want == "error" || want == "partial" || want == "unknown") {
				t.Fatalf("result=%+v err=%v pushes=%v creates=%d", result, err, runner.pushes, creates)
			}
			if mode == "reuse" && !result.Reused {
				t.Fatal("existing PR was not reused")
			}
			if (mode == "wait" || mode == "create" || mode == "reuse") && (result.Inspection == nil || result.Inspection.Status != "ready") {
				t.Fatalf("inspection=%+v", result.Inspection)
			}
			if len(runner.pushes) > 0 && runner.pushes[0][len(runner.pushes[0])-1] != runner.sha+":refs/heads/"+runner.branch {
				t.Fatalf("push was not pinned: %v", runner.pushes)
			}
		})
	}
}

type mergeCleanupWorldAPI struct {
	API
	world         *cleanupWorld
	merged        bool
	mergeRequests int
	fork          bool
	requests      []string
}

func (api *mergeCleanupWorldAPI) Do(ctx context.Context, method, endpoint string, payload, target any) (int, error) {
	api.requests = append(api.requests, endpoint)
	var value any
	switch {
	case strings.HasPrefix(endpoint, "repos/owner/repo/branches/"):
		branch, _ := url.PathUnescape(strings.TrimPrefix(endpoint, "repos/owner/repo/branches/"))
		sha, err := api.world.remoteRunner.Run(ctx, nil, "git", "rev-parse", "--verify", "refs/heads/"+branch)
		if err != nil {
			return 0, &sdk.ErrorResponse{Response: &http.Response{StatusCode: 404}, Message: "Not Found"}
		}
		value = map[string]any{"name": branch, "protected": api.world.protected[branch], "commit": map[string]any{"sha": strings.TrimSpace(string(sha))}}
	case endpoint == "repos/owner/repo/pulls/7":
		pr := api.world.prs[0]
		if api.fork {
			pr.Head.Repo = &struct {
				Name string `json:"full_name"`
			}{Name: "fork/repo"}
		}
		value = pr
	case endpoint == "graphql" && strings.HasPrefix(payload.(map[string]any)["query"].(string), "query MergePullRequest"):
		state := "OPEN"
		if api.merged {
			state = "MERGED"
		}
		value = map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"url": "https://github.com/owner/repo/pull/7", "state": state, "isDraft": false, "merged": api.merged, "headRefOid": api.world.base, "baseRefOid": api.world.base, "mergeable": "MERGEABLE", "mergeStateStatus": "CLEAN", "reviewDecision": "APPROVED", "mergeCommit": map[string]any{"oid": api.world.base}}}}}
	case strings.Contains(endpoint, "/check-runs?"):
		value = map[string]any{"check_runs": []map[string]any{{"id": 1, "name": "test", "status": "completed", "conclusion": "success"}}}
	case strings.Contains(endpoint, "/statuses?"):
		value = []any{}
	case method == "PUT" && strings.HasSuffix(endpoint, "/merge-async"):
		api.mergeRequests++
		api.merged = true
		api.world.prs[0].State = "closed"
		stamp := "2026-10-08T00:00:00Z"
		api.world.prs[0].MergedAt = &stamp
		value = map[string]any{"status": "merged", "details": map[string]any{"sha": api.world.base}}
	default:
		return api.API.Do(ctx, method, endpoint, payload, target)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return 0, json.Unmarshal(data, target)
}

func TestMergeCleanupOnlyRemovesMergedPRBranch(t *testing.T) {
	for _, mode := range []string{"cleanup", "no cleanup", "fork", "current branch"} {
		t.Run(mode, func(t *testing.T) {
			world := newCleanupWorld(t)
			world.branch("7-fix-workflows", true, true)
			world.branch("8-fix-unrelated", true, true)
			world.pr(7, "7-fix-workflows", "open", false)
			world.pr(8, "8-fix-unrelated", "closed", true)
			if mode == "current branch" {
				world.git("switch", "7-fix-workflows")
			}
			api := &mergeCleanupWorldAPI{API: world.client.API, world: world, fork: mode == "fork"}
			world.client.API = api
			options := quickMergeOptions()
			options.Cleanup = mode != "no cleanup"
			result, err := world.client.MergePullRequest(context.Background(), "owner/repo", options)
			want := "merged"
			if mode == "current branch" {
				want = "partial"
			}
			if err != nil || result.Status != want || result.MergeSHA == "" || api.mergeRequests != 1 {
				t.Fatalf("result=%+v err=%v requests=%d", result, err, api.mergeRequests)
			}
			for _, runner := range []directoryRunner{world.runner.directoryRunner, world.remoteRunner} {
				if _, err := runner.Run(context.Background(), nil, "git", "rev-parse", "--verify", "refs/heads/8-fix-unrelated"); err != nil {
					t.Fatal("cleanup removed unrelated finished work")
				}
				_, err := runner.Run(context.Background(), nil, "git", "rev-parse", "--verify", "refs/heads/7-fix-workflows")
				if (err != nil) != (mode == "cleanup") {
					t.Fatalf("source branch existence mode=%s error=%v", mode, err)
				}
			}
		})
	}
}
