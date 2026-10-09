package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type noCleanupRunner struct{ t *testing.T }

func (runner noCleanupRunner) Run(context.Context, []byte, string, ...string) ([]byte, error) {
	runner.t.Fatal("merge that was not confirmed attempted Git cleanup")
	return nil, nil
}

func TestMergeCleanupRequiresConfirmedMerge(t *testing.T) {
	for _, mode := range []string{"blocked", "pending checks", "accepted only", "API error"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/owner/repo/pulls/7":
					fmt.Fprint(w, `{"number":7,"head":{"ref":"fix/workflows","sha":"head1","repo":{"full_name":"owner/repo"}}}`)
				case r.URL.Path == "/graphql":
					if mode == "API error" {
						w.WriteHeader(403)
						fmt.Fprint(w, `{"message":"forbidden"}`)
					} else if mode == "blocked" {
						mergeFixturePR(w, map[string]any{"isDraft": true})
					} else {
						mergeFixturePR(w, nil)
					}
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					if mode == "pending checks" {
						fmt.Fprint(w, `{"check_runs":[{"id":1,"name":"test","status":"in_progress"}]}`)
					} else {
						mergeFixtureChecks(w)
					}
				case strings.HasSuffix(r.URL.Path, "/statuses"):
					fmt.Fprint(w, `[]`)
				case strings.Contains(r.URL.Path, "/merge-async"):
					w.WriteHeader(202)
					fmt.Fprint(w, `{"status":"pending","details":{"uuid":"req1"}}`)
				default:
					t.Errorf("unconfirmed merge fetched cleanup data: %s", r.URL)
					w.WriteHeader(500)
				}
			})
			f.client.Runner = noCleanupRunner{t}
			options := quickMergeOptions()
			options.Cleanup, options.Timeout = true, 20*time.Millisecond
			result, _ := f.client.MergePullRequest(context.Background(), "owner/repo", options)
			want := "timeout"
			if mode == "blocked" {
				want = "blocked"
			} else if mode == "API error" {
				want = "error"
			}
			if result.Status != want || result.Merged || result.Cleanup != nil || result.MergeRequested != (mode == "accepted only") {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestScopedCleanupFetchesOnlyTargetBranchAndPRs(t *testing.T) {
	world := newCleanupWorld(t)
	world.branch("7-fix-workflows", true, true)
	world.branch("8-fix-unrelated", true, true)
	world.pr(7, "7-fix-workflows", "closed", true)
	world.pr(8, "8-fix-unrelated", "closed", true)
	api := &mergeCleanupWorldAPI{API: world.client.API, world: world}
	world.client.API = api
	plan, err := world.client.PlanCleanup(context.Background(), "owner/repo", CleanupOptions{Remote: "origin", Scope: "both", Branch: "7-fix-workflows"})
	if err != nil || len(plan.Targets) != 2 {
		t.Fatalf("plan=%+v error=%v", plan, err)
	}
	branchReads, prReads := 0, 0
	for _, endpoint := range api.requests {
		if endpoint == "graphql" || strings.HasPrefix(endpoint, "repos/owner/repo/branches?") {
			t.Fatalf("scoped cleanup read full inventory: %s", endpoint)
		}
		if endpoint == "repos/owner/repo/branches/7-fix-workflows" {
			branchReads++
		}
		if strings.Contains(endpoint, "/pulls?") {
			prReads++
			if !strings.Contains(endpoint, "head=owner%3A7-fix-workflows") {
				t.Fatalf("PR read was not scoped: %s", endpoint)
			}
		}
	}
	if branchReads != 1 || prReads != 1 {
		t.Fatalf("branch reads=%d PR reads=%d", branchReads, prReads)
	}
	for _, target := range plan.Targets {
		if target.RemoteBranch != "7-fix-workflows" {
			t.Fatalf("included unrelated branch: %+v", target)
		}
	}
}

func TestSubmitRejectsBaseAndPolicyErrorsBeforePush(t *testing.T) {
	for _, mode := range []string{"base mismatch", "missing label", "missing base"} {
		t.Run(mode, func(t *testing.T) {
			runner := &submitTestRunner{sha: strings.Repeat("a", 40), branch: "fix/workflows", remote: "https://github.com/owner/repo.git"}
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/pulls" {
					if mode == "base mismatch" {
						fmt.Fprintf(w, `[{"number":7,"state":"open","html_url":"https://github.com/owner/repo/pull/7","base":{"ref":"release"},"head":{"ref":"fix/workflows","sha":%q,"repo":{"full_name":"owner/repo"}}}]`, runner.sha)
					} else {
						fmt.Fprint(w, `[]`)
					}
					return
				}
				if strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/ref/heads/") {
					w.WriteHeader(404)
					fmt.Fprint(w, `{"message":"Not Found"}`)
					return
				}
				if !commonResponse(w, r) {
					t.Errorf("unexpected request %s", r.URL)
					w.WriteHeader(500)
				}
			})
			f.client.Runner = runner
			spec := Spec{Prefix: "fix", Title: "improve workflows", Body: "수정 내용", Base: "main"}
			if mode == "missing label" {
				labels := []string{"does-not-exist"}
				spec.Labels = &labels
			}
			result, err := f.client.SubmitPR(context.Background(), "owner/repo", SubmitOptions{Spec: spec, Policy: DefaultPolicy()})
			if err == nil || result.PushAttempted || len(runner.pushes) > 0 || len(f.writes()) > 0 {
				t.Fatalf("invalid request mutated before validation: %+v err=%v", result, err)
			}
		})
	}
}

func TestReviewWarmCacheOnlyFetchesChangedBodies(t *testing.T) {
	for _, mode := range []string{"unchanged", "edited", "head changed", "missing body"} {
		t.Run(mode, func(t *testing.T) {
			bodies := 0
			known := ReviewComment{ID: "C1", Body: "cached source", DiffHunk: "original hunk", UpdatedAt: "2026-10-08T00:00:00Z"}
			updated := known.UpdatedAt
			if mode == "edited" || mode == "missing body" {
				updated = "2026-10-08T01:00:00Z"
			}
			head := "abc"
			if mode == "head changed" {
				head = "new"
			}
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/graphql" {
					var payload struct {
						Query     string         `json:"query"`
						Variables map[string]any `json:"variables"`
					}
					_ = json.NewDecoder(r.Body).Decode(&payload)
					if strings.HasPrefix(payload.Query, "query ReviewCommentBodies") {
						bodies++
						if mode == "missing body" {
							fmt.Fprint(w, `{"data":{"nodes":[null]}}`)
							return
						}
						fmt.Fprintf(w, `{"data":{"nodes":[{"id":"C1","body":"fresh source","diff_hunk":"fresh hunk","updated_at":%q}]}}`, updated)
						return
					}
					if strings.Contains(payload.Query, "diff_hunk:diffHunk") || strings.Contains(payload.Query, " body ") {
						t.Error("warm metadata lookup requested unchanged bodies")
					}
					thread := reviewFixtureThread("T1", false, false, ReviewComment{ID: "C1", UpdatedAt: updated})
					reviewFixturePR(w, head, []graphReviewThread{thread}, reviewPageInfo{})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/reviews") {
					fmt.Fprint(w, `[]`)
					return
				}
				fmt.Fprintf(w, `{"head":{"sha":%q}}`, head)
			})
			result, err := f.client.PullRequestReviews(context.Background(), "owner/repo", ReviewOptions{Number: 7, CachedHead: "abc", CachedComments: map[string]ReviewComment{"C1": known}})
			if err != nil {
				t.Fatal(err)
			}
			wantBodies := 1
			if mode == "unchanged" {
				wantBodies = 0
			}
			if bodies != wantBodies || result.Complete != (mode != "missing body") {
				t.Fatalf("result=%+v body reads=%d", result, bodies)
			}
			if mode == "unchanged" && result.Threads[0].Comments[0].Body != "cached source" {
				t.Fatal("lost unchanged cached body")
			}
			if (mode == "edited" || mode == "head changed") && result.Threads[0].Comments[0].Body != "fresh source" {
				t.Fatal("used stale body")
			}
		})
	}
}

func TestInspectSectionsAndCompletedFailureReuse(t *testing.T) {
	jobs, attempt := 0, 2
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if !strings.HasPrefix(payload["query"].(string), "query MergePullRequest") {
				t.Error("checks/failures selection fetched reviews")
			}
			mergeFixturePR(w, nil)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			fmt.Fprint(w, `{"check_runs":[{"id":1,"name":"test","status":"completed","conclusion":"failure","app":{"slug":"github-actions"},"details_url":"https://github.com/owner/repo/actions/runs/42/job/11"}]}`)
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			fmt.Fprint(w, `[]`)
		case r.URL.Path == "/repos/owner/repo/actions/runs/42":
			fmt.Fprintf(w, `{"id":42,"head_sha":"head1","run_attempt":%d,"status":"completed","conclusion":"failure"}`, attempt)
		case strings.Contains(r.URL.Path, "/attempts/"):
			jobs++
			fmt.Fprint(w, `{"jobs":[{"id":11,"name":"test","status":"completed","conclusion":"failure","steps":[]}]}`)
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	})
	f.client.API = rerunFixtureLogs{API: f.client.API}
	checks, err := f.client.InspectPR(context.Background(), "owner/repo", InspectOptions{Number: 7, Sections: "checks"})
	if err != nil || !checks.Complete || checks.Reviews != nil || len(checks.Runs) != 0 || jobs != 0 {
		t.Fatalf("checks=%+v err=%v jobs=%d", checks, err, jobs)
	}
	first, err := f.client.InspectPR(context.Background(), "owner/repo", InspectOptions{Number: 7, Sections: "failures"})
	if err != nil || !first.Complete || len(first.Failures) != 1 || jobs != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := f.client.InspectPR(context.Background(), "owner/repo", InspectOptions{Number: 7, Sections: "failures", CachedFailures: first.Failures})
	if err != nil || !second.Complete || second.ReusedEvidence != 1 || jobs != 1 {
		t.Fatalf("cached=%+v err=%v jobs=%d", second, err, jobs)
	}
	attempt = 3
	third, err := f.client.InspectPR(context.Background(), "owner/repo", InspectOptions{Number: 7, Sections: "failures", CachedFailures: first.Failures})
	if err != nil || third.ReusedEvidence != 0 || jobs != 2 {
		t.Fatalf("new attempt=%+v err=%v jobs=%d", third, err, jobs)
	}
}

func TestCITestFactsAndTimestampDedupPreserveVariants(t *testing.T) {
	first := []string{"2026-10-08T00:00:00Z --- FAIL: TestEquality (0.01s)", "2026-10-08T00:00:00Z     sample_test.go:12: expected 20, actual 10"}
	second := []string{"2026-10-08T01:00:00Z --- FAIL: TestEquality (0.01s)", "2026-10-08T01:00:00Z     sample_test.go:12: expected 20, actual 10"}
	var evidence []CIEvidence
	a := ciExtractEvidence(first, false)
	a.Occurrences = []CIOccurrence{{JobID: 1}}
	b := ciExtractEvidence(second, false)
	b.Occurrences = []CIOccurrence{{JobID: 2}}
	ciAppendEvidence(&evidence, a)
	ciAppendEvidence(&evidence, b)
	if len(evidence) != 1 || evidence[0].Kind != "test_failure" || len(evidence[0].Tests) != 1 || evidence[0].Tests[0].Name != "TestEquality" || len(evidence[0].Occurrences) != 2 || len(evidence[0].LogVariants) != 1 || strings.Join(evidence[0].LogVariants[0].Lines, "\n") != strings.Join(second, "\n") {
		t.Fatalf("dedup lost original evidence: %+v", evidence)
	}
	pytest := ciExtractEvidence([]string{"FAILED tests/test_api.py::test_login - AssertionError: expected 200"}, false)
	if len(pytest.Tests) != 1 || pytest.Tests[0].Framework != "pytest" || pytest.Tests[0].Path != "tests/test_api.py" || pytest.Tests[0].Message != "AssertionError: expected 200" {
		t.Fatalf("pytest facts=%+v", pytest)
	}
	different := ciExtractEvidence([]string{"2026-10-08T01:00:00Z --- FAIL: TestEquality (0.01s)", "2026-10-08T01:00:00Z     sample_test.go:12: expected 30, actual 10"}, false)
	ciAppendEvidence(&evidence, different)
	if len(evidence) != 2 {
		t.Fatal("different assertion messages were merged")
	}
}
