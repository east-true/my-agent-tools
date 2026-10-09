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

func workflowReviewFixture(w http.ResponseWriter, head, decision string, threads []graphReviewThread) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"url": "https://github.com/owner/repo/pull/7", "head_sha": head, "review_decision": decision, "reviewThreads": map[string]any{"nodes": threads, "pageInfo": reviewPageInfo{}}}}}})
}

func TestInspectCombinesReviewsChecksRunAndExactAttemptEvidence(t *testing.T) {
	jobReads := 0
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/graphql":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if strings.HasPrefix(payload["query"].(string), "query PullRequestReviews") {
				workflowReviewFixture(w, "head1", "APPROVED", []graphReviewThread{reviewFixtureThread("T1", false, true, ReviewComment{ID: "C1", Body: "fix the test"})})
			} else {
				mergeFixturePR(w, nil)
			}
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			fmt.Fprint(w, `{"check_runs":[{"id":9,"name":"test","status":"completed","conclusion":"failure","app":{"slug":"github-actions"},"details_url":"https://github.com/owner/repo/actions/runs/42/job/11","output":{"summary":"tests failed"}},{"id":10,"name":"lint","status":"completed","conclusion":"success","app":{"slug":"github-actions"},"details_url":"https://github.com/owner/repo/actions/runs/42/job/12"}]}`)
		case strings.HasSuffix(r.URL.Path, "/statuses") || strings.HasSuffix(r.URL.Path, "/reviews"):
			fmt.Fprint(w, `[]`)
		case r.URL.Path == "/repos/owner/repo/actions/runs/42":
			fmt.Fprint(w, `{"id":42,"run_attempt":2,"head_sha":"head1","status":"completed","conclusion":"failure"}`)
		case r.URL.Path == "/repos/owner/repo/actions/runs/42/attempts/2/jobs":
			jobReads++
			fmt.Fprint(w, `{"jobs":[{"id":11,"name":"test","status":"completed","conclusion":"failure","steps":[]}]}`)
		case r.URL.Path == "/repos/owner/repo/pulls/7":
			fmt.Fprint(w, `{"head":{"sha":"head1"}}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(500)
		}
	})
	f.client.API = rerunFixtureLogs{API: f.client.API}
	result, err := f.client.InspectPR(context.Background(), "owner/repo", InspectOptions{Number: 7})
	if err != nil || !result.Complete || result.Status != "blocked" || len(result.Checks) != 2 || len(result.Runs) != 1 || len(result.Failures) != 1 || result.Failures[0].Run.Attempt != 2 || jobReads != 1 || result.Reviews == nil || len(result.Reviews.Threads) != 1 {
		t.Fatalf("result=%+v err=%v job reads=%d", result, err, jobReads)
	}
	if len(f.writes()) != 0 || result.Reasons[0].Details != "tests failed" {
		t.Fatalf("mutation or lost reason: %+v", result.Reasons)
	}
}

func TestInspectWaitAndPartialSnapshots(t *testing.T) {
	for _, mode := range []string{"ready", "wait", "timeout", "head changed", "review unavailable", "base changed", "test commit changed"} {
		t.Run(mode, func(t *testing.T) {
			checkReads, prReads := 0, 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/graphql":
					var payload map[string]any
					_ = json.NewDecoder(r.Body).Decode(&payload)
					if strings.HasPrefix(payload["query"].(string), "query PullRequestReviews") {
						if mode == "review unavailable" {
							fmt.Fprint(w, `{"errors":[{"message":"denied"}]}`)
						} else {
							workflowReviewFixture(w, "head1", "APPROVED", []graphReviewThread{})
						}
					} else {
						prReads++
						edits := map[string]any{}
						if prReads > 1 {
							switch mode {
							case "head changed":
								edits["headRefOid"] = "new"
							case "base changed":
								edits["baseRefOid"] = "new"
							case "test commit changed":
								edits["potentialMergeCommit"] = map[string]any{"oid": "new"}
							}
						}
						mergeFixturePR(w, edits)
					}
				case strings.HasSuffix(r.URL.Path, "/check-runs"):
					checkReads++
					if mode == "timeout" || (mode == "wait" && checkReads == 1) {
						fmt.Fprint(w, `{"check_runs":[{"id":1,"name":"test","status":"queued"}]}`)
					} else {
						mergeFixtureChecks(w)
					}
				case strings.HasSuffix(r.URL.Path, "/statuses") || strings.HasSuffix(r.URL.Path, "/reviews"):
					fmt.Fprint(w, `[]`)
				case r.URL.Path == "/repos/owner/repo/pulls/7":
					fmt.Fprint(w, `{"head":{"sha":"head1"}}`)
				default:
					t.Errorf("unexpected request %s", r.URL)
				}
			})
			options := InspectOptions{Number: 7, Timeout: 2 * time.Second, Interval: time.Millisecond, Wait: mode == "wait" || mode == "timeout"}
			if mode == "timeout" {
				options.Timeout, options.Interval = 100*time.Millisecond, time.Second
			}
			result, err := f.client.InspectPR(context.Background(), "owner/repo", options)
			want := "partial"
			if mode == "ready" || mode == "wait" {
				want = "ready"
			}
			if mode == "timeout" {
				want = "timeout"
			}
			if err != nil || result.Status != want || result.Complete != (want == "ready") || len(f.writes()) != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if mode == "wait" && checkReads != 2 {
				t.Fatalf("check reads=%d", checkReads)
			}
		})
	}
}

func TestDeltaPaginatesCommitsAndMakesPatchOptional(t *testing.T) {
	since, head, mid := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	for _, patch := range []bool{false, true} {
		t.Run(fmt.Sprint(patch), func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/graphql" {
					mergeFixturePR(w, map[string]any{"headRefOid": head})
					return
				}
				if !strings.Contains(r.URL.Path, "/compare/") {
					t.Errorf("unexpected request: %s", r.URL)
					return
				}
				if r.URL.Query().Get("page") == "1" {
					w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
					fmt.Fprintf(w, `{"status":"ahead","base_commit":{"sha":%q},"merge_base_commit":{"sha":%q},"total_commits":2,"commits":[{"sha":%q}],"files":[{"filename":"z.go","previous_filename":"old.go","status":"renamed","additions":1,"deletions":0,"patch":"+large"},{"filename":"a.bin","status":"modified","additions":0,"deletions":0}]}`, since, since, mid)
				} else {
					fmt.Fprintf(w, `{"status":"ahead","base_commit":{"sha":%q},"merge_base_commit":{"sha":%q},"total_commits":2,"commits":[{"sha":%q}]}`, since, since, head)
				}
			})
			result, err := f.client.PullRequestDelta(context.Background(), "owner/repo", DeltaOptions{Number: 7, Since: since, IncludePatch: patch})
			if err != nil || !result.Complete || len(result.Files) != 2 || result.Files[0].Filename != "a.bin" || result.Files[1].PreviousFilename != "old.go" || (result.Files[1].Patch != nil) != patch {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestDeltaRejectsIncompleteAndChangedBaselines(t *testing.T) {
	since, head := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, mode := range []string{"identical", "diverged", "incomplete commits", "file limit", "head changed"} {
		t.Run(mode, func(t *testing.T) {
			prReads, compares := 0, 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/graphql" {
					prReads++
					sha := head
					if mode == "identical" {
						sha = since
					}
					if mode == "head changed" && prReads > 1 {
						sha = strings.Repeat("c", 40)
					}
					mergeFixturePR(w, map[string]any{"headRefOid": sha})
					return
				}
				compares++
				status, total := "ahead", 1
				if mode == "diverged" {
					status = "diverged"
				}
				if mode == "incomplete commits" {
					total = 2
				}
				files := []DeltaFile{}
				if mode == "file limit" {
					for i := 0; i < 300; i++ {
						files = append(files, DeltaFile{Filename: fmt.Sprint(i), Status: "modified"})
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "total_commits": total, "base_commit": map[string]any{"sha": since}, "merge_base_commit": map[string]any{"sha": since}, "commits": []map[string]any{{"sha": head}}, "files": files})
			})
			result, err := f.client.PullRequestDelta(context.Background(), "owner/repo", DeltaOptions{Number: 7, Since: since})
			if mode == "incomplete commits" {
				if err == nil {
					t.Fatal("accepted missing commits")
				}
				return
			}
			want := "partial"
			if mode == "identical" {
				want = "ok"
				if compares != 0 {
					t.Fatal("identical head was needlessly compared")
				}
			}
			if mode == "diverged" {
				want = "needs_refresh"
			}
			if err != nil || result.Status != want || result.Complete != (mode == "identical") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
