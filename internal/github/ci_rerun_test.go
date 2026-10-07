package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func quickRerunOptions() CIRerunOptions {
	return CIRerunOptions{RunID: 42, Wait: true, Timeout: 2 * time.Second, Interval: time.Millisecond, MaxLogBytes: 8 << 20}
}

func rerunFixtureRun(w http.ResponseWriter, attempt int, status, conclusion string) {
	_ = json.NewEncoder(w).Encode(CIRun{ID: 42, Attempt: attempt, HeadSHA: "abc", Status: status, Conclusion: conclusion, URL: "https://github.com/owner/repo/actions/runs/42"})
}

func TestRerunWaitsForNextAttemptAndRequestsOnlyOnce(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprint(all), func(t *testing.T) {
			reads, posts := 0, 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					want := "/repos/owner/repo/actions/runs/42/rerun-failed-jobs"
					if all {
						want = "/repos/owner/repo/actions/runs/42/rerun"
					}
					if r.URL.Path != want {
						t.Errorf("endpoint=%s want=%s", r.URL.Path, want)
					}
					w.WriteHeader(201)
					return
				}
				reads++
				switch reads {
				case 1, 2, 3:
					conclusion := "failure"
					if all {
						conclusion = "success"
					}
					rerunFixtureRun(w, 1, "completed", conclusion)
				case 4:
					rerunFixtureRun(w, 2, "queued", "")
				case 5:
					rerunFixtureRun(w, 2, "in_progress", "")
				default:
					rerunFixtureRun(w, 2, "completed", "success")
				}
			})
			options := quickRerunOptions()
			options.All = all
			result, err := f.client.RerunCI(context.Background(), "owner/repo", options)
			if err != nil || result.Status != "completed" || !result.RerunRequested || !result.RequestAttempted || result.Run.Attempt != 2 || result.PreviousAttempt != 1 || result.ExpectedAttempt != 2 || posts != 1 || reads != 6 {
				t.Fatalf("result=%+v err=%v reads=%d posts=%d", result, err, reads, posts)
			}
		})
	}
}

type rerunFixtureLogs struct {
	API
	err error
}

func (api rerunFixtureLogs) CIJobLog(context.Context, string, int64, int64) (string, bool, error) {
	return "Error: rerun failure\nkeep full log", false, api.err
}

func TestFailedRerunPinsDiagnosticsToObservedAttempt(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			posts, reads := 0, 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					w.WriteHeader(201)
					return
				}
				if r.URL.Path == "/repos/owner/repo/actions/runs/42/attempts/2/jobs" {
					fmt.Fprint(w, `{"jobs":[{"id":11,"name":"test","status":"completed","conclusion":"failure","steps":[{"name":"test","number":1,"conclusion":"failure"}]}]}`)
					return
				}
				if r.URL.Path != "/repos/owner/repo/actions/runs/42" {
					t.Errorf("unexpected diagnostic endpoint: %s", r.URL)
					w.WriteHeader(500)
					return
				}
				reads++
				if posts == 0 {
					rerunFixtureRun(w, 1, "completed", "failure")
				} else if reads == 3 {
					rerunFixtureRun(w, 2, "completed", "failure")
				} else {
					// A new external rerun must never replace attempt 2 diagnostics.
					rerunFixtureRun(w, 3, "completed", "success")
				}
			})
			logs := rerunFixtureLogs{API: f.client.API}
			if unavailable {
				logs.err = errors.New("logs expired")
			}
			f.client.API = logs
			result, err := f.client.RerunCI(context.Background(), "owner/repo", quickRerunOptions())
			if err != nil || result.Status != "failed" || result.Failure == nil || result.Failure.Run.Attempt != 2 || result.Failure.Run.HeadSHA != "abc" || reads != 3 || posts != 1 {
				t.Fatalf("result=%+v err=%v reads=%d posts=%d", result, err, reads, posts)
			}
			if unavailable {
				if result.Failure.Complete || result.Failure.Status != "partial" || len(result.Failure.Jobs[0].Notes) == 0 {
					t.Fatalf("missing partial evidence: %+v", result.Failure)
				}
			} else if len(result.Failure.Evidence) != 1 || !strings.Contains(strings.Join(result.Failure.Evidence[0].Lines, "\n"), "keep full log") {
				t.Fatalf("missing full evidence: %+v", result.Failure)
			}
		})
	}
}

func TestRerunPreflightAndNoWait(t *testing.T) {
	for _, mode := range []string{"dry run", "active", "successful", "changed", "no wait", "incomplete"} {
		t.Run(mode, func(t *testing.T) {
			reads := 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					w.WriteHeader(201)
					return
				}
				reads++
				switch mode {
				case "active":
					rerunFixtureRun(w, 1, "in_progress", "")
				case "successful":
					rerunFixtureRun(w, 1, "completed", "success")
				case "changed":
					if reads > 1 {
						rerunFixtureRun(w, 2, "queued", "")
					} else {
						rerunFixtureRun(w, 1, "completed", "failure")
					}
				case "incomplete":
					fmt.Fprint(w, `{"id":42,"run_attempt":1,"status":"completed","head_sha":"abc"}`)
				default:
					rerunFixtureRun(w, 1, "completed", "failure")
				}
			})
			options := quickRerunOptions()
			options.DryRun = mode == "dry run"
			options.Wait = mode != "no wait"
			result, err := f.client.RerunCI(context.Background(), "owner/repo", options)
			want, writes := "blocked", 0
			if mode == "dry run" {
				want = "planned"
			}
			if mode == "no wait" {
				want, writes = "requested", 1
			}
			if mode == "incomplete" {
				want = "error"
			}
			if result.Status != want || len(f.writes()) != writes || (err != nil) != (mode == "incomplete") {
				t.Fatalf("result=%+v err=%v writes=%+v", result, err, f.writes())
			}
		})
	}
}

func TestRerunTimeoutUnknownAndConcurrentAttemptsNeverRetry(t *testing.T) {
	for _, mode := range []string{"timeout", "cancelled", "post error", "forbidden", "poll error", "superseded", "head changed"} {
		t.Run(mode, func(t *testing.T) {
			posts := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					if mode == "post error" || mode == "forbidden" {
						status := 503
						if mode == "forbidden" {
							status = 403
						}
						w.WriteHeader(status)
						fmt.Fprint(w, `{"message":"request failed"}`)
						return
					}
					w.WriteHeader(201)
					return
				}
				if posts > 0 {
					switch mode {
					case "cancelled":
						cancel()
					case "poll error":
						w.WriteHeader(503)
						fmt.Fprint(w, `{"message":"unavailable"}`)
						return
					case "superseded":
						rerunFixtureRun(w, 3, "completed", "success")
						return
					case "head changed":
						fmt.Fprint(w, `{"id":42,"run_attempt":2,"head_sha":"other","status":"completed","conclusion":"success"}`)
						return
					}
				}
				rerunFixtureRun(w, 1, "completed", "failure")
			})
			options := quickRerunOptions()
			if mode == "timeout" {
				options.Timeout = 100 * time.Millisecond
				options.Interval = time.Second
			}
			result, err := f.client.RerunCI(ctx, "owner/repo", options)
			want := "unknown"
			if mode == "timeout" || mode == "cancelled" || mode == "superseded" {
				want = mode
			}
			if mode == "forbidden" {
				want = "error"
			}
			if result.Status != want || posts != 1 || !result.RequestAttempted || (result.RerunRequested != (mode != "post error" && mode != "forbidden")) || (err == nil && mode != "superseded") {
				t.Fatalf("result=%+v err=%v posts=%d", result, err, posts)
			}
		})
	}
}
