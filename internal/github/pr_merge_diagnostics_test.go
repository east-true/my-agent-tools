package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func mergeDiagnosticsFixture(t *testing.T, mode string) (Client, *ciLogFixture, map[string]int) {
	t.Helper()
	reads := map[string]int{}
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		reads[r.URL.Path]++
		switch {
		case r.URL.Path == "/graphql":
			edits := map[string]any{}
			if mode == "test commit" {
				edits["potentialMergeCommit"] = map[string]any{"oid": "test1"}
			}
			mergeFixturePR(w, edits)
		case strings.HasSuffix(r.URL.Path, "/check-runs") && strings.Contains(r.URL.Path, "/commits/"):
			checks := []map[string]any{}
			count := 1
			if mode == "matrix" {
				count = 2
			}
			for i := 0; i < count; i++ {
				check := map[string]any{"id": 91 + i, "name": fmt.Sprintf("test-%d", i), "status": "completed", "conclusion": "failure", "output": map[string]any{"summary": "Tests failed", "text": strings.Repeat("원본 검사 설명\n", 500)}, "app": map[string]any{"slug": "github-actions"}, "details_url": fmt.Sprintf("https://github.com/owner/repo/actions/runs/42/job/%d", 11+i)}
				if mode == "external" || mode == "annotation failure" {
					check["app"] = map[string]any{"slug": "external-ci"}
				} else if mode == "foreign link" {
					check["details_url"] = "https://github.com/other/repo/actions/runs/42/job/11"
				}
				checks = append(checks, check)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"check_runs": checks})
		case strings.HasSuffix(r.URL.Path, "/statuses"):
			fmt.Fprint(w, `[]`)
		case strings.HasPrefix(r.URL.Path, "/repos/owner/repo/actions/jobs/"):
			id := 11
			if strings.HasSuffix(r.URL.Path, "/12") {
				id = 12
			}
			job := mergeDiagnosticJob{ID: int64(id), RunID: 42, Attempt: 2, HeadSHA: "head1", CheckRunURL: fmt.Sprintf("https://api.github.com/repos/owner/repo/check-runs/%d", id+80)}
			if mode == "job head changed" {
				job.HeadSHA = "foreign"
			} else if mode == "job check changed" {
				job.CheckRunURL = "https://api.github.com/repos/owner/repo/check-runs/99"
			} else if mode == "test commit" {
				job.HeadSHA = "test1"
			}
			_ = json.NewEncoder(w).Encode(job)
		case r.URL.Path == "/repos/owner/repo/actions/runs/42/attempts/2" || r.URL.Path == "/repos/owner/repo/actions/runs/42":
			run := CIRun{ID: 42, Attempt: 2, HeadSHA: "head1", Status: "completed", Conclusion: "failure"}
			if mode == "run head changed" {
				run.HeadSHA = "foreign"
			} else if mode == "test commit" {
				run.HeadSHA = "test1"
			} else if mode == "newer attempt" && r.URL.Path == "/repos/owner/repo/actions/runs/42" {
				run.Attempt = 3
			}
			_ = json.NewEncoder(w).Encode(run)
		case r.URL.Path == "/repos/owner/repo/actions/runs/42/attempts/2/jobs":
			if mode == "jobs failure" {
				w.WriteHeader(403)
				fmt.Fprint(w, `{"message":"Jobs unavailable"}`)
				return
			}
			id := 11
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/actions/runs/42/attempts/2/jobs?page=2>; rel="next"`, r.Host))
			} else {
				id = 12
			}
			if mode == "missing job" && id == 11 {
				fmt.Fprint(w, `{"jobs":[]}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"jobs": []CIJob{{ID: int64(id), Name: fmt.Sprintf("test-%d", id), Status: "completed", Conclusion: "failure", CheckRunURL: fmt.Sprintf("https://api.github.com/repos/owner/repo/check-runs/%d", id+80), Steps: []CIStep{{Number: 2, Name: "Run tests", Conclusion: "failure"}}}}})
		case strings.HasSuffix(r.URL.Path, "/annotations"):
			if mode == "annotation failure" || mode == "annotations forbidden" {
				w.WriteHeader(403)
				fmt.Fprint(w, `{"message":"Annotations unavailable"}`)
				return
			}
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
				fmt.Fprint(w, `[{"path":"tests/test_api.py","start_line":12,"annotation_level":"failure","message":"assertion mismatch"}]`)
			} else {
				fmt.Fprint(w, `[{"path":"tests/test_api.py","start_line":90,"annotation_level":"warning","message":"second page"}]`)
			}
		default:
			t.Errorf("unexpected merge diagnostic request %s", r.URL)
			w.WriteHeader(500)
		}
	})
	log := "setup\n--- FAIL: TestLogin (0.01s)\nFAILED tests/test_api.py::test_login - AssertionError: expected 200\n" + strings.Repeat("additional context\n", 100) + "LAST ORIGINAL LOG LINE\n"
	logs := &ciLogFixture{API: f.client.API, logs: map[int64]string{11: log, 12: log}, truncated: mode == "truncated"}
	if mode == "expired logs" {
		logs.logError = errors.New("HTTP 410")
	}
	f.client.API, f.client.Runner = logs, noCleanupRunner{t}
	return f.client, logs, reads
}

func TestMergeUsesStandaloneCollectorOncePerAttempt(t *testing.T) {
	client, logs, reads := mergeDiagnosticsFixture(t, "matrix")
	result, err := client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
	if err != nil || result.Status != "blocked" || len(result.Reasons) != 2 || len(result.Failures) != 1 || !result.Failures[0].Complete || !reflect.DeepEqual(logs.downloads, []int64{11, 12}) {
		t.Fatalf("result=%+v err=%v downloads=%v", result, err, logs.downloads)
	}
	if reads["/repos/owner/repo/actions/runs/42/attempts/2"] != 1 || reads["/repos/owner/repo/actions/runs/42/attempts/2/jobs"] != 2 || reads["/repos/owner/repo/check-runs/91/annotations"] != 2 || reads["/repos/owner/repo/check-runs/92/annotations"] != 2 {
		t.Fatalf("repeated attempt collection or incomplete pagination: %v", reads)
	}
	for _, reason := range result.Reasons {
		if reason.RunID != 42 || reason.RunAttempt != 2 || reason.Check == nil || !reason.Check.Complete || reason.DiagnosticError != "" || len(reason.Check.Output.Text) < 2000 {
			t.Fatalf("lost complete source or attempt reference: %+v", reason)
		}
	}
	failure := result.Failures[0]
	if len(failure.Jobs) != 2 || len(failure.Jobs[0].Annotations) != 2 || len(failure.Evidence) != 1 || len(failure.Evidence[0].Tests) != 2 || failure.Evidence[0].Lines[len(failure.Evidence[0].Lines)-1] != "LAST ORIGINAL LOG LINE" || len(failure.Evidence[0].Occurrences) != 2 {
		t.Fatalf("lost paged annotations, test facts or original context: %+v", failure)
	}
	standalone, err := client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: true})
	if err != nil || !reflect.DeepEqual(standalone, failure) {
		t.Fatalf("standalone collector differs: err=%v standalone=%+v merge=%+v", err, standalone, failure)
	}
}

func TestMergeDiagnosticsPreserveFailureAndPinHistoricalAttempt(t *testing.T) {
	for _, mode := range []string{"newer attempt", "test commit", "truncated", "expired logs", "jobs failure", "annotations forbidden", "job head changed", "job check changed", "run head changed", "foreign link", "missing job"} {
		t.Run(mode, func(t *testing.T) {
			client, logs, reads := mergeDiagnosticsFixture(t, mode)
			result, err := client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
			complete := mode == "newer attempt" || mode == "test commit"
			if err != nil || result.Status != "blocked" || result.MergeRequested || len(result.Reasons) != 1 || result.Reasons[0].Code != "check_failed" || result.Reasons[0].Check.Complete != complete || (result.Reasons[0].DiagnosticError == "") != complete {
				t.Fatalf("lost original failure or diagnostics status: %+v err=%v", result, err)
			}
			if mode == "job head changed" || mode == "job check changed" || mode == "run head changed" || mode == "foreign link" {
				if len(logs.downloads) != 0 || len(result.Failures) != 0 {
					t.Fatal("unverified workflow logs were collected")
				}
			} else if len(result.Failures) != 1 || result.Failures[0].Run.Attempt != 2 || result.Failures[0].Complete != complete {
				t.Fatalf("wrong attempt/completeness: %+v", result.Failures)
			}
			if reads["/repos/owner/repo/actions/runs/42"] != 0 {
				t.Fatal("merge fetched the latest attempt instead of the failed check's historical attempt")
			}
		})
	}
}

func TestMergeExternalChecksUseSharedAnnotationPagination(t *testing.T) {
	for _, mode := range []string{"external", "annotation failure"} {
		t.Run(mode, func(t *testing.T) {
			client, logs, _ := mergeDiagnosticsFixture(t, mode)
			result, err := client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
			if err != nil || result.Status != "blocked" || len(result.Failures) != 0 || len(logs.downloads) != 0 {
				t.Fatalf("external check confused with Actions: %+v err=%v", result, err)
			}
			check := result.Reasons[0].Check
			if check.Complete != (mode == "external") || (len(check.Annotations) == 2) != (mode == "external") {
				t.Fatalf("annotation paging/completeness lost: %+v", check)
			}
		})
	}
	client, _, reads := mergeDiagnosticsFixture(t, "external")
	options := quickMergeOptions()
	options.Annotations = false
	result, err := client.MergePullRequest(context.Background(), "owner/repo", options)
	if err != nil || !result.Reasons[0].Check.Complete || reads["/repos/owner/repo/check-runs/91/annotations"] != 0 {
		t.Fatalf("--annotations=false fetched annotations: %+v err=%v", result, err)
	}
}
