package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/east-true/my-agent-tools/internal/github"
	"github.com/tiktoken-go/tokenizer"
)

func TestCompactReviewsRetainsChangedAndOutdatedCoordinatesAndEdits(t *testing.T) {
	line, original, start := 10, 8, 5
	value := github.ReviewResult{Status: "partial", Complete: false, Notes: []string{"history unavailable"}, Threads: []github.ReviewThread{
		{ID: "current", Path: "a.go", Line: &line, OriginalLine: &line, StartLine: &start, OriginalStartLine: &start, DiffSide: "RIGHT", StartDiffSide: "RIGHT",
			Comments: []github.ReviewComment{{ID: "C1", Body: "exact request", CreatedAt: "same", UpdatedAt: "same", URL: "comment-url", Author: &github.ReviewAuthor{Login: "alice"}}}},
		{ID: "outdated", Path: "b.go", Outdated: true, OriginalLine: &original, OriginalStartLine: &start, DiffSide: "LEFT", StartDiffSide: "RIGHT",
			Comments: []github.ReviewComment{{ID: "C2", Body: "edited request", CreatedAt: "before", UpdatedAt: "after"}}},
	}}
	for i := 0; i < 18; i++ {
		thread := value.Threads[0]
		thread.ID = fmt.Sprintf("current-%d", i)
		value.Threads = append(value.Threads, thread)
	}
	raw, err := compactJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := compactValue(value, compactFlags{Enabled: true, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := projected.(map[string]any)
	if !ok {
		t.Fatal("expected redundant metadata to be compacted")
	}
	threads := result["threads"].([]any)
	current, outdated := threads[0].(map[string]any), threads[1].(map[string]any)
	for _, key := range []string{"original_line", "original_start_line", "start_diff_side"} {
		if _, exists := current[key]; exists {
			t.Fatalf("redundant coordinate retained: %s", key)
		}
	}
	comment := current["comments"].([]any)[0].(map[string]any)
	if _, exists := comment["updated_at"]; exists {
		t.Fatal("unchanged timestamp retained")
	}
	if current["line"] != json.Number("10") || current["start_line"] != json.Number("5") || current["diff_side"] != "RIGHT" || comment["body"] != "exact request" || comment["url"] != "comment-url" || comment["author"].(map[string]any)["login"] != "alice" {
		t.Fatalf("current context changed: %+v", current)
	}
	if line, exists := outdated["line"]; !exists || line != nil {
		t.Fatal("outdated current line must remain explicitly null")
	}
	if outdated["original_line"] != json.Number("8") || outdated["original_start_line"] != json.Number("5") || outdated["start_diff_side"] != "RIGHT" || outdated["is_outdated"] != true || outdated["comments"].([]any)[0].(map[string]any)["updated_at"] != "after" {
		t.Fatalf("outdated or edited context lost: %+v", outdated)
	}
	if result["status"] != "partial" || result["complete"] != false {
		t.Fatal("partial collection outcome changed")
	}
	preserved, err := os.ReadFile(result["evidence_file"].(string))
	if err != nil || !bytes.Equal(bytes.TrimSuffix(preserved, []byte("\n")), raw) {
		t.Fatalf("exact original metadata not retained: %v", err)
	}
}

func TestCompactReviewProjectionDoesNotStripOtherEvidence(t *testing.T) {
	value := map[string]any{"id": "diagnostic", "path": "a.go", "comments": []any{}, "original_line": nil, "updated_at": "same", "created_at": "same"}
	projected := compactProjection(value).(map[string]any)
	if _, exists := projected["original_line"]; !exists || projected["updated_at"] != "same" {
		t.Fatal("non-review evidence changed")
	}
}

// Compares outputs captured from the frozen old CLI and the current CLI.
// No model calls: these are serialized-output counts in explicit encodings.
func TestReviewMetadataOutputComparison(t *testing.T) {
	dir := os.Getenv("TOOLS_REVIEW_METADATA_MEASUREMENT_DIR")
	if dir == "" {
		t.Skip("set measurement directory for a frozen CLI comparison")
	}
	rows := []map[string]any{}
	for _, encoding := range []tokenizer.Encoding{tokenizer.O200kBase, tokenizer.Cl100kBase} {
		before, err := os.ReadFile(filepath.Join(dir, string(encoding)+"-before.json"))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(dir, string(encoding)+"-after.json"))
		if err != nil {
			t.Fatal(err)
		}
		var oldValue, newValue map[string]any
		if json.Unmarshal(before, &oldValue) != nil || json.Unmarshal(after, &newValue) != nil {
			t.Fatal("invalid captured JSON")
		}
		if oldValue["evidence_sha256"] != newValue["evidence_sha256"] || oldValue["evidence_file"] != newValue["evidence_file"] {
			t.Fatal("comparison changed full evidence or artifact reference")
		}
		codec, err := tokenizer.Get(encoding)
		if err != nil {
			t.Fatal(err)
		}
		beforeTokens, err := codec.Count(string(bytes.TrimSpace(before)))
		if err != nil {
			t.Fatal(err)
		}
		afterTokens, err := codec.Count(string(bytes.TrimSpace(after)))
		if err != nil {
			t.Fatal(err)
		}
		if len(after) >= len(before) || afterTokens >= beforeTokens {
			t.Fatal("review metadata output did not shrink")
		}
		rows = append(rows, map[string]any{"encoding": encoding, "before_tokens": beforeTokens, "after_tokens": afterTokens, "before_bytes": len(bytes.TrimSpace(before)), "after_bytes": len(bytes.TrimSpace(after))})
		t.Logf("%s: tokens %d -> %d", encoding, beforeTokens, afterTokens)
	}
	data, err := json.MarshalIndent(map[string]any{"model_calls": 0, "agent_task_tokens_measured": false, "measurements": rows}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "measurement.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
