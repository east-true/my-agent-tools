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

func mergeFixturePR(w http.ResponseWriter, edits map[string]any) {
	pr := map[string]any{"url": "https://github.com/owner/repo/pull/7", "state": "OPEN", "isDraft": false, "merged": false, "headRefOid": "head1", "baseRefOid": "base1", "mergeable": "MERGEABLE", "mergeStateStatus": "CLEAN", "reviewDecision": "APPROVED"}
	for k, v := range edits {
		pr[k] = v
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": pr}}})
}

func quickMergeOptions() MergeOptions {
	return MergeOptions{Number: 7, Timeout: 2 * time.Second, Interval: time.Millisecond, Annotations: true}
}

func mergeFixtureChecks(w http.ResponseWriter) {
	fmt.Fprint(w, `{"check_runs":[{"id":1,"name":"test","status":"completed","conclusion":"success"}]}`)
}

func TestMergeWaitsForChecksAndActualAsyncCompletion(t *testing.T) {
	checkReads, requestReads := 0, 0
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql":
			mergeFixturePR(w, nil)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			checkReads++
			if checkReads == 1 {
				fmt.Fprint(w, `{"check_runs":[{"id":1,"name":"test","status":"in_progress"}]}`)
			} else {
				mergeFixtureChecks(w)
			}
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			fmt.Fprint(w, `[]`)
		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/merge-async"):
			if r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
				t.Error("missing merge API version")
			}
			w.WriteHeader(202)
			fmt.Fprint(w, `{"status":"pending","details":{"uuid":"request-1"}}`)
		case r.URL.Path == "/repos/owner/repo/pulls/7/merge-async/request-1":
			requestReads++
			if requestReads == 1 {
				fmt.Fprint(w, `{"status":"pending","details":{"uuid":"request-1"}}`)
			} else {
				fmt.Fprint(w, `{"status":"merged","details":{"sha":"merged1"}}`)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	})
	result, err := f.client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
	if err != nil || result.Status != "merged" || result.MergeSHA != "merged1" || checkReads != 2 || requestReads != 2 {
		t.Fatalf("result=%+v err=%v checks=%d merge polls=%d", result, err, checkReads, requestReads)
	}
	writes := f.writes()
	if len(writes) != 1 {
		t.Fatalf("merge mutations=%+v", writes)
	}
	for key, want := range map[string]any{"sha": "head1", "merge_method": "squash", "merge_action": "default", "bypass_rules": false} {
		if writes[0].body[key] != want {
			t.Errorf("payload %s=%v want %v", key, writes[0].body[key], want)
		}
	}
}

type fixtureJobLogs struct {
	API
	calls int
}

func (api *fixtureJobLogs) CIJobLog(context.Context, string, int64, int64) (string, bool, error) {
	api.calls++
	return "setup output\n2026-10-07T00:00:00Z ##[error]expected 200, received 500\n" + strings.Repeat("unrelated output\n", 1000), false, nil
}

func TestMergeFailureCollectsAnnotationsStepsAndLogEvidence(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/graphql":
			mergeFixturePR(w, nil)
		case "/repos/owner/repo/commits/head1/check-runs":
			fmt.Fprint(w, `{"check_runs":[{"id":9,"name":"test","status":"completed","conclusion":"failure","app":{"slug":"github-actions"},"details_url":"https://github.com/owner/repo/actions/runs/4/job/42","output":{"title":"Tests failed","summary":"Login test failed"}}]}`)
		case "/repos/owner/repo/commits/head1/statuses":
			fmt.Fprint(w, `[]`)
		case "/repos/owner/repo/check-runs/9/annotations":
			fmt.Fprint(w, `[{"path":"login_test.go","start_line":12,"annotation_level":"failure","message":"response mismatch"}]`)
		case "/repos/owner/repo/actions/jobs/42":
			fmt.Fprint(w, `{"id":42,"run_id":4,"run_attempt":2,"head_sha":"head1","check_run_url":"https://api.github.com/repos/owner/repo/check-runs/9"}`)
		case "/repos/owner/repo/actions/runs/4/attempts/2":
			fmt.Fprint(w, `{"id":4,"run_attempt":2,"head_sha":"head1","status":"completed","conclusion":"failure"}`)
		case "/repos/owner/repo/actions/runs/4/attempts/2/jobs":
			fmt.Fprint(w, `{"jobs":[{"id":42,"status":"completed","conclusion":"failure","check_run_url":"https://api.github.com/repos/owner/repo/check-runs/9","steps":[{"name":"Run tests","conclusion":"failure"}]}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(500)
		}
	})
	logs := &fixtureJobLogs{API: f.client.API}
	f.client.API = logs
	result, err := f.client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
	if err != nil || result.Status != "blocked" || len(result.Reasons) != 1 || len(f.writes()) != 0 || logs.calls != 1 {
		t.Fatalf("result=%+v err=%v log calls=%d", result, err, logs.calls)
	}
	r := result.Reasons[0]
	for _, want := range []string{"Login test failed", "login_test.go:12", "Run tests", "expected 200, received 500"} {
		if !strings.Contains(r.Details, want) {
			t.Errorf("missing %s: %+v", want, r)
		}
	}
	data, _ := json.Marshal(result)
	if !strings.Contains(string(data), "unrelated output") || len(result.Failures) != 1 || !result.Failures[0].Complete || result.Failures[0].Run.Attempt != 2 || r.RunID != 4 || r.RunAttempt != 2 || r.Check == nil || !r.Check.Complete {
		t.Fatalf("shared failure evidence or exact attempt was lost: %+v", result)
	}
}

func TestMergeBlockersAndHeadChangesNeverSubmit(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		edits      map[string]any
	}{
		{"draft", "draft", map[string]any{"isDraft": true}},
		{"closed", "pr_closed", map[string]any{"state": "CLOSED"}},
		{"conflict", "merge_conflict", map[string]any{"mergeable": "CONFLICTING"}},
		{"behind", "branch_behind", map[string]any{"mergeStateStatus": "BEHIND"}},
		{"approval", "review_required", map[string]any{"reviewDecision": "REVIEW_REQUIRED"}},
		{"changes", "changes_requested", map[string]any{"reviewDecision": "CHANGES_REQUESTED"}},
		{"new head", "head_changed", map[string]any{"headRefOid": "head2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/graphql":
					reads++
					if tc.code == "head_changed" && reads == 1 {
						mergeFixturePR(w, nil)
					} else {
						mergeFixturePR(w, tc.edits)
					}
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					mergeFixtureChecks(w)
				case strings.HasSuffix(r.URL.Path, "/statuses"):
					fmt.Fprint(w, `[]`)
				default:
					t.Errorf("unexpected request %s", r.URL)
					w.WriteHeader(500)
				}
			})
			result, err := f.client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
			if err != nil || result.Status != "blocked" || len(result.Reasons) == 0 || result.Reasons[0].Code != tc.code || len(f.writes()) != 0 {
				t.Fatalf("result=%+v err=%v writes=%v", result, err, f.writes())
			}
		})
	}
}

func TestMergeTimeoutReportsPendingWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name, checks, statuses, code string
		edits                        map[string]any
	}{
		{"missing", `{"check_runs":[]}`, `[]`, "checks_missing", nil},
		{"running", `{"check_runs":[{"id":1,"name":"test","status":"queued"}]}`, `[]`, "check_pending", nil},
		{"status", `{"check_runs":[]}`, `[{"id":1,"context":"external-ci","state":"pending"}]`, "status_pending", nil},
		{"mergeability", `{"check_runs":[{"id":1,"name":"test","status":"completed","conclusion":"success"}]}`, `[]`, "mergeability_pending", map[string]any{"mergeable": "UNKNOWN"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/graphql":
					mergeFixturePR(w, tc.edits)
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					fmt.Fprint(w, tc.checks)
				case strings.HasSuffix(r.URL.Path, "/statuses"):
					fmt.Fprint(w, tc.statuses)
				default:
					t.Errorf("unexpected %s", r.URL)
					w.WriteHeader(500)
				}
			})
			options := quickMergeOptions()
			options.Timeout = 200 * time.Millisecond
			options.Interval = time.Second
			result, err := f.client.MergePullRequest(context.Background(), "owner/repo", options)
			if err != nil || result.Status != "timeout" || len(result.Reasons) != 1 || result.Reasons[0].Code != tc.code || len(f.writes()) != 0 {
				t.Fatalf("result=%+v err=%v writes=%v", result, err, f.writes())
			}
		})
	}
}

func TestMergeChecksPaginationKeepsLatestStatusesAndEveryCheck(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			if r.URL.Query().Get("page") == "1" {
				mergeFixtureChecks(w)
			} else {
				fmt.Fprint(w, `{"check_runs":[{"id":2,"name":"optional","status":"completed","conclusion":"failure"}]}`)
			}
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			if r.URL.Query().Get("page") == "1" {
				fmt.Fprint(w, `[{"id":2,"context":"external-ci","state":"success"}]`)
			} else {
				fmt.Fprint(w, `[{"id":1,"context":"external-ci","state":"failure"},{"id":3,"context":"security","state":"error","description":"scanner unavailable"}]`)
			}
		default:
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(500)
		}
	})
	checks, statuses, err := f.client.mergeChecks(context.Background(), "owner/repo", "head1")
	failed, pending := checkReasons(checks, statuses)
	if err != nil || len(checks) != 2 || len(statuses) != 2 || len(failed) != 2 || len(pending) != 0 {
		t.Fatalf("checks=%+v statuses=%+v failed=%+v err=%v", checks, statuses, failed, err)
	}
	for _, r := range failed {
		if r.Name == "external-ci" {
			t.Fatal("obsolete failed status was used")
		}
	}
}

func TestMergeUsesTestCommitChecksAndFallsBackToHead(t *testing.T) {
	for _, testHasChecks := range []bool{false, true} {
		t.Run(fmt.Sprint(testHasChecks), func(t *testing.T) {
			headRead := false
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/statuses"):
					fmt.Fprint(w, `[]`)
				case strings.Contains(r.URL.Path, "/commits/test1/"):
					if testHasChecks {
						mergeFixtureChecks(w)
					} else {
						fmt.Fprint(w, `{"check_runs":[]}`)
					}
				case strings.Contains(r.URL.Path, "/commits/head1/"):
					headRead = true
					mergeFixtureChecks(w)
				default:
					t.Errorf("unexpected %s", r.URL)
					w.WriteHeader(500)
				}
			})
			pr := &mergePR{HeadSHA: "head1"}
			pr.TestCommit = &struct {
				OID string `json:"oid"`
			}{"test1"}
			checks, _, err := f.client.prMergeChecks(context.Background(), "owner/repo", pr)
			if err != nil || len(checks) != 1 || headRead == testHasChecks {
				t.Fatalf("checks=%v err=%v head=%t", checks, err, headRead)
			}
		})
	}
}

func TestMergeQueueWaitsUntilMergedAndNeverReportsEnqueueAsSuccess(t *testing.T) {
	for _, merged := range []bool{false, true} {
		t.Run(fmt.Sprint(merged), func(t *testing.T) {
			submitted, queuePolls := false, 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/graphql":
					edits := map[string]any{}
					if submitted {
						queuePolls++
						edits["mergeQueueEntry"] = map[string]any{"id": "queue1"}
						if merged && queuePolls >= 2 {
							edits["merged"] = true
							edits["state"] = "MERGED"
							edits["mergeCommit"] = map[string]any{"oid": "merge1"}
						}
					}
					mergeFixturePR(w, edits)
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					mergeFixtureChecks(w)
				case strings.HasSuffix(r.URL.Path, "/statuses"):
					fmt.Fprint(w, `[]`)
				case r.Method == "PUT":
					submitted = true
					fmt.Fprint(w, `{"status":"enqueued","details":{}}`)
				default:
					t.Errorf("unexpected %s", r.URL)
					w.WriteHeader(500)
				}
			})
			options := quickMergeOptions()
			if !merged {
				options.Timeout = 100 * time.Millisecond
			}
			result, err := f.client.MergePullRequest(context.Background(), "owner/repo", options)
			want := "timeout"
			if merged {
				want = "merged"
			}
			if err != nil || result.Status != want || !result.Queued || len(f.writes()) != 1 {
				t.Fatalf("result=%+v err=%v writes=%v", result, err, f.writes())
			}
		})
	}
}

func TestMergeRejectionAndUnknownOutcomeAreNotRetried(t *testing.T) {
	for _, status := range []int{403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/graphql":
					mergeFixturePR(w, nil)
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					mergeFixtureChecks(w)
				case strings.HasSuffix(r.URL.Path, "/statuses"):
					fmt.Fprint(w, `[]`)
				case r.Method == "PUT":
					w.WriteHeader(status)
					fmt.Fprint(w, `{"message":"repository rule rejected merge"}`)
				default:
					t.Errorf("unexpected %s", r.URL)
					w.WriteHeader(500)
				}
			})
			result, err := f.client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
			want := "blocked"
			if status == 500 {
				want = "unknown"
			}
			if result.Status != want || len(f.writes()) != 1 || !result.MergeRequested || len(result.Reasons) == 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if status == 500 && err == nil {
				t.Fatal("unknown outcome must preserve API error")
			}
		})
	}
}

func TestMergeAsyncFailureFetchesDiagnosticState(t *testing.T) {
	submitted := false
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql":
			edits := map[string]any{}
			if submitted {
				edits["reviewDecision"] = "REVIEW_REQUIRED"
			}
			mergeFixturePR(w, edits)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			mergeFixtureChecks(w)
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			fmt.Fprint(w, `[]`)
		case r.Method == "PUT":
			submitted = true
			fmt.Fprint(w, `{"status":"pending","details":{"uuid":"req1"}}`)
		case strings.HasSuffix(r.URL.Path, "/merge-async/req1"):
			fmt.Fprint(w, `{"status":"failed","details":{"message":"Required approving review is missing"}}`)
		default:
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(500)
		}
	})
	result, err := f.client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
	if err != nil || result.Status != "blocked" || len(result.Reasons) != 2 || result.Reasons[1].Code != "review_required" || len(f.writes()) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestMergeDiagnosticFailurePreservesKnownFailure(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql":
			mergeFixturePR(w, nil)
		case strings.Contains(r.URL.Path, "/commits/") && strings.HasSuffix(r.URL.Path, "/check-runs"):
			fmt.Fprint(w, `{"check_runs":[{"id":1,"name":"test","status":"completed","conclusion":"timed_out"}]}`)
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(403)
			fmt.Fprint(w, `{"message":"diagnostic read denied"}`)
		}
	})
	result, err := f.client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
	if err != nil || result.Status != "blocked" || len(result.Reasons) != 1 || result.Reasons[0].DiagnosticError == "" || result.Reasons[0].Code != "check_failed" || len(f.writes()) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestMergeAcceptedRequestTimeoutKeepsRecoveryFields(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql":
			mergeFixturePR(w, nil)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			mergeFixtureChecks(w)
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			fmt.Fprint(w, `[]`)
		case r.Method == "PUT":
			w.WriteHeader(202)
			fmt.Fprint(w, `{"status":"pending","details":{"uuid":"req1"}}`)
		default:
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(500)
		}
	})
	options := quickMergeOptions()
	options.Timeout = 200 * time.Millisecond
	options.Interval = time.Second
	result, err := f.client.MergePullRequest(context.Background(), "owner/repo", options)
	if err != nil || result.Status != "timeout" || !result.MergeRequested || result.RequestID != "req1" || len(result.Reasons) != 1 || result.Reasons[0].Code != "merge_pending" || len(f.writes()) != 1 {
		t.Fatalf("lost accepted request: result=%+v err=%v writes=%v", result, err, f.writes())
	}
}

func TestMergeInvalidMetadataAndReadErrorsNeverSubmit(t *testing.T) {
	for _, body := range []string{`{"errors":[{"message":"permission denied"}]}`, `{"data":{"repository":null}}`, `{"data":{"repository":{"pullRequest":{"state":"OPEN"}}}}`} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			result, err := f.client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
			if err == nil || result.Status != "error" || len(f.writes()) != 0 || len(result.Reasons) != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestMergeAlreadyMergedAndCancellation(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		mergeFixturePR(w, map[string]any{"merged": true, "state": "MERGED", "mergeCommit": map[string]any{"oid": "merge1"}})
	})
	result, err := f.client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
	if err != nil || result.Status != "merged" || result.MergeSHA != "merge1" || len(f.writes()) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = f.client.MergePullRequest(ctx, "owner/repo", quickMergeOptions())
	if err != nil || result.Status != "cancelled" || result.MergeRequested {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestMergeReadableEvidenceAndActionsURLParsing(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want int64
	}{
		{"https://github.com/owner/repo/actions/runs/4/job/42", 42},
		{"https://github.com/owner/repo/actions/runs/4", 0},
		{"https://example.com/owner/repo/actions/runs/4/job/42", 0},
		{"https://github.com/owner/repo/actions/runs/4/job/-1", 0},
	} {
		if got := actionsJobID(tc.url); got != tc.want {
			t.Errorf("actionsJobID(%s)=%d", tc.url, got)
		}
	}
	evidence := mergeFailureSummary(CIFailureResult{Evidence: []CIEvidence{{Lines: strings.Split("2026-10-07T00:00:00Z \x1b[31m##[error]failure\x1b[0m\n"+strings.Repeat("Error: "+strings.Repeat("x", 500)+"\n", 100), "\n"), Occurrences: []CIOccurrence{{JobID: 1}}}}}, CIJob{ID: 1})
	if !strings.Contains(evidence, "failure") || strings.Contains(evidence, "\x1b") || len([]rune(evidence)) > 1501 {
		t.Fatalf("unbounded or malformed evidence: %q", evidence)
	}
}
