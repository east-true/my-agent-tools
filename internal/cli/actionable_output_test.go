package cli

import (
	"bytes"
	"encoding/json"
	"github.com/east-true/my-agent-tools/internal/github"
	"strings"
	"testing"
)

func TestCompactPreservesLongReviewAndDiagnosticTailWithoutArtifactLookup(t *testing.T) {
	body := strings.Repeat("review explanation ", 150) + "Required final change: retry_budget=9"
	hunk := strings.Repeat("+ neutral context\n", 150) + "+ retry_budget=3\n"
	message := strings.Repeat("diagnostic detail ", 150) + "missing method Flush"
	value := map[string]any{"status": "partial", "complete": false, "body": body, "diff_hunk": hunk, "message": message}
	projected, err := compactValue(value, compactFlags{Enabled: true, Dir: t.TempDir()})
	raw, _ := compactJSON(projected)
	var decoded map[string]any
	json.Unmarshal(raw, &decoded)
	if err != nil || decoded["body"] != body || decoded["diff_hunk"] != hunk || decoded["message"] != message || decoded["complete"] != false {
		t.Fatal("actionable original text lost", err)
	}
	for _, key := range []string{"body_truncated", "message_truncated"} {
		if decoded[key] != nil {
			t.Fatal("exact field marked truncated", key)
		}
	}
}

func TestInspectionDeltaReturnsCurrentOutstandingWorkAfterContextLoss(t *testing.T) {
	result := stateFixtureResult()
	line := 12
	result.PR.BaseSHA = "base"
	result.Reviews.Threads[0].Line = &line
	result.Reviews.Threads[0].Comments[0].DiffHunk = "+ change here"
	result.Checks = append(result.Checks, github.PRCheck{ID: 2, Kind: "check", Status: "completed", Conclusion: "neutral"}, github.PRCheck{ID: 3, Kind: "status", Status: "success"})
	result.Reviews.Threads = append(result.Reviews.Threads, github.ReviewThread{ID: "resolved", Resolved: true, Comments: []github.ReviewComment{{Body: "old resolved history"}}})
	before := inspectionItems(result)
	output, _ := inspectionDelta(result, &before, false, "state.json")
	data := output.(map[string]any)
	work := data["outstanding"].(*inspectionWork)
	if data["status"] != "unchanged" || len(work.Checks) != 1 || len(work.Threads) != 1 || work.PR.BaseSHA != "base" || *work.Threads[0].Line != 12 || work.Threads[0].Comments[0].Body != "request" || work.Threads[0].Comments[0].DiffHunk != "+ change here" {
		t.Fatal("context loss requires another lookup", data)
	}
	result.Reviews.Threads[0].Comments[0].Body = "updated request"
	changed, _ := inspectionDelta(result, &before, false, "state.json")
	raw, _ := json.Marshal(changed)
	if bytes.Contains(raw, []byte(`"body":"request"`)) || !bytes.Contains(raw, []byte("updated request")) || bytes.Contains(raw, []byte("old resolved history")) {
		t.Fatal("outstanding work replayed stale or resolved context", string(raw))
	}
	result.Checks = nil
	result.Reviews = nil
	result.Status = "ready"
	result.Reasons = nil
	ready, _ := inspectionDelta(result, &before, false, "state.json")
	if ready.(map[string]any)["outstanding"] != nil {
		t.Fatal("ready result replayed old unresolved work")
	}
}
