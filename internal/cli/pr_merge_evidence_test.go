package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type mergeEvidenceCLIAPI struct {
	mergeCLIAPI
	annotationReads int
	limits          []int64
}

func (api *mergeEvidenceCLIAPI) Do(ctx context.Context, method, endpoint string, payload, target any) (int, error) {
	var data any
	switch {
	case strings.Contains(endpoint, "/check-runs?"):
		data = map[string]any{"check_runs": []map[string]any{{"id": 9, "name": "test", "status": "completed", "conclusion": "failure", "app": map[string]any{"slug": "github-actions"}, "details_url": "https://github.com/owner/repo/actions/runs/42/job/11", "output": map[string]any{"text": strings.Repeat("검사 원문 설명과 문맥을 유지합니다. ", 300)}}}}
	case endpoint == "repos/owner/repo/actions/jobs/11":
		data = map[string]any{"id": 11, "run_id": 42, "run_attempt": 2, "head_sha": "head1", "check_run_url": "https://api.github.com/repos/owner/repo/check-runs/9"}
	case endpoint == "repos/owner/repo/actions/runs/42/attempts/2":
		data = map[string]any{"id": 42, "run_attempt": 2, "head_sha": "head1", "status": "completed", "conclusion": "failure"}
	case strings.Contains(endpoint, "/attempts/2/jobs?"):
		data = map[string]any{"jobs": []map[string]any{{"id": 11, "status": "completed", "conclusion": "failure", "check_run_url": "https://api.github.com/repos/owner/repo/check-runs/9", "steps": []map[string]any{{"name": "tests", "conclusion": "failure"}}}}}
	case strings.Contains(endpoint, "/annotations?"):
		api.annotationReads++
		data = []map[string]any{{"path": "test.py", "start_line": 12, "annotation_level": "failure", "message": "expected 200"}}
	default:
		return api.mergeCLIAPI.Do(ctx, method, endpoint, payload, target)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	return 0, json.Unmarshal(raw, target)
}

func (api *mergeEvidenceCLIAPI) CIJobLog(_ context.Context, _ string, _ int64, limit int64) (string, bool, error) {
	api.limits = append(api.limits, limit)
	var log strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&log, "processing %03d: additional original job context\n", i)
	}
	log.WriteString("FAILED tests/test_api.py::test_login - AssertionError: expected 200\nlast original job context")
	text := log.String()
	if int64(len(text)) > limit {
		return text[:limit], true, nil
	}
	return text, false, nil
}

func TestMergeCLISharedEvidencePreservesFullResultAndCompactArtifact(t *testing.T) {
	for _, mode := range []string{"full", "compact", "default", "annotations disabled", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			api := &mergeEvidenceCLIAPI{}
			var out, stderr bytes.Buffer
			args := []string{"github", "pr", "merge", "--number", "7", "--repo", "owner/repo", "--cleanup=false", "--json", "--artifact-dir", filepath.Join(t.TempDir(), "evidence")}
			if mode != "default" {
				args = append(args, "--compact=false")
			}
			if mode == "compact" {
				args = append(args, "--compact")
			} else if mode == "annotations disabled" {
				args = append(args, "--annotations=false")
			} else if mode == "truncated" {
				args = append(args, "--max-log-bytes", "100")
			}
			code := run(context.Background(), args, nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return api, nil })
			if code != 1 || stderr.Len() != 0 || api.writes != 0 || len(api.limits) != 1 {
				t.Fatalf("code=%d out=%s stderr=%s limits=%v", code, out.String(), stderr.String(), api.limits)
			}
			var projection struct {
				EvidenceFile   string `json:"evidence_file"`
				EvidenceSHA256 string `json:"evidence_sha256"`
			}
			if err := json.Unmarshal(out.Bytes(), &projection); err != nil {
				t.Fatal(err)
			}
			data := out.Bytes()
			if mode == "compact" || mode == "default" {
				var err error
				data, err = os.ReadFile(projection.EvidenceFile)
				if err != nil || fmt.Sprintf("%x", sha256.Sum256(bytes.TrimSuffix(data, []byte("\n")))) != projection.EvidenceSHA256 || len(out.Bytes()) >= len(data) || !strings.Contains(out.String(), `"text_truncated":true`) {
					t.Fatalf("compact evidence lost original source: out=%s err=%v", out.String(), err)
				}
			} else if projection.EvidenceFile != "" {
				t.Fatal("full output unexpectedly created a compact artifact")
			}
			var result github.MergeResult
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != "blocked" || len(result.Failures) != 1 || result.Failures[0].Run.Attempt != 2 || len(result.Reasons[0].Check.Output.Text) < 2000 || result.Reasons[0].Code != "check_failed" {
				t.Fatalf("merge outcome or full check source changed: %+v", result)
			}
			if mode == "truncated" {
				if api.limits[0] != 100 || result.Failures[0].Complete || !result.Failures[0].Evidence[0].Truncated || result.Reasons[0].DiagnosticError == "" {
					t.Fatalf("log cap was not marked partial: %+v", result)
				}
			} else if !result.Failures[0].Complete || !bytes.Contains(data, []byte("last original job context")) || len(result.Failures[0].Evidence[0].Tests) != 1 {
				t.Fatal("shared collector lost original context or test facts")
			}
			if (api.annotationReads == 0) != (mode == "annotations disabled") {
				t.Fatalf("annotation selection ignored: reads=%d", api.annotationReads)
			}
		})
	}
}
