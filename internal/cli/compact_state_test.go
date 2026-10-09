package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/github"
)

func TestCompactEvidencePreservesRawResultAndActualLineNumbers(t *testing.T) {
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = fmt.Sprintf("<setup> & line %d: %s", i+1, strings.Repeat("unrelated", 8))
	}
	lines[150] = "AssertionError: expected 20, actual 10"
	value := map[string]any{"id": int64(9223372036854775807), "status": "failed", "complete": true, "rerun_requested": true, "evidence": []any{map[string]any{"kind": "log", "lines": lines}}, "body": strings.Repeat("요청", 1000), "diff_hunk": strings.Repeat("+large diff\n", 500)}
	raw, _ := compactJSON(value)
	projected, err := compactValue(value, compactFlags{Enabled: true, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	result := projected.(map[string]any)
	compact, _ := compactJSON(projected)
	if !bytes.Contains(compact, []byte(`"id":9223372036854775807`)) {
		t.Fatal("compact output changed a large database ID")
	}
	if len(compact) >= len(raw) || result["status"] != "failed" || result["complete"] != true || result["rerun_requested"] != true || result["body_truncated"] != true {
		t.Fatalf("result not reduced without changing operation state: %s", compact)
	}
	preserved, err := os.ReadFile(result["evidence_file"].(string))
	if err != nil || !bytes.Equal(bytes.TrimSuffix(preserved, []byte("\n")), raw) || result["evidence_sha256"] != fmt.Sprintf("%x", sha256.Sum256(raw)) {
		t.Fatalf("full evidence was not retained: %v", err)
	}
	evidence := result["evidence"].([]any)[0].(map[string]any)
	found := false
	for _, excerpt := range evidence["excerpts"].([]map[string]any) {
		start := excerpt["start_line"].(int)
		for i, line := range excerpt["lines"].([]any) {
			if line != lines[start+i-1] {
				t.Fatal("excerpt lost original line coordinates")
			}
			found = found || line == lines[150]
		}
	}
	if !found || evidence["omitted_lines"].(int) <= 0 {
		t.Fatalf("missing failure context: %+v", evidence)
	}
}

func TestCompactNeverExpandsSmallOutputOrDropsUnknownEvidence(t *testing.T) {
	dir := t.TempDir()
	small := map[string]any{"status": "ok", "body": "short", "diff_hunk": "tiny"}
	result, err := compactValue(small, compactFlags{Enabled: true, Dir: dir})
	before, _ := compactJSON(small)
	after, _ := compactJSON(result)
	entries, _ := os.ReadDir(dir)
	if err != nil || !bytes.Equal(before, after) || len(entries) != 0 {
		t.Fatalf("small result changed: %s err=%v", after, err)
	}
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("unknown runner output %d %s", i+1, strings.Repeat("context ", 20))
	}
	large := map[string]any{"status": "partial", "lines": lines}
	result, err = compactValue(large, compactFlags{Enabled: true, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := compactJSON(result)
	if !bytes.Contains(data, []byte(lines[0])) || !bytes.Contains(data, []byte(lines[99])) || !bytes.Contains(data, []byte(`"omitted_lines":90`)) {
		t.Fatalf("unknown evidence lacked visible boundaries: %s", data)
	}
	if _, err := os.Stat(result.(map[string]any)["evidence_file"].(string)); err != nil {
		t.Fatal(err)
	}
	longLine := map[string]any{"status": "failed", "lines": []string{strings.Repeat("large diagnostic ", 1000)}}
	result, err = compactValue(longLine, compactFlags{Enabled: true, Dir: dir})
	if err != nil || result.(map[string]any)["lines_truncated"] != true {
		t.Fatalf("single huge diagnostic line was not marked and compacted: %v", err)
	}
}

func TestCompactStorageFailurePreservesMutationOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	value := map[string]any{"status": "merged", "merge_requested": true, "body": strings.Repeat("long evidence ", 1000)}
	var out, stderr bytes.Buffer
	code := encodeCompact(&out, &stderr, value, compactFlags{Enabled: true, Dir: path})
	if code != 1 || !strings.Contains(out.String(), `"status":"merged"`) || !strings.Contains(out.String(), `"merge_requested":true`) || stderr.Len() == 0 {
		t.Fatalf("lost actual mutation result: code=%d out=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func stateFixtureResult() github.InspectResult {
	return github.InspectResult{Status: "blocked", Repo: "owner/repo", Number: 7, Complete: true, PR: github.PRState{HeadSHA: "abc"}, Checks: []github.PRCheck{{ID: 1, Kind: "check", Name: "test", Status: "completed", Conclusion: "failure"}}, Reviews: &github.ReviewResult{Threads: []github.ReviewThread{{ID: "T1", Path: "main.go", Comments: []github.ReviewComment{{ID: "C1", Body: "request"}}}}}}
}

func TestInspectionDeltaOnlyEmitsChangedEntitiesAndPreservesOutstandingWork(t *testing.T) {
	result := stateFixtureResult()
	before := inspectionItems(result)
	output, _ := inspectionDelta(result, &before, false, "state.json")
	unchanged := output.(map[string]any)
	if unchanged["status"] != "unchanged" || unchanged["attention_required"] != true || unchanged["pr_status"] != "blocked" {
		t.Fatalf("unchanged work misrepresented: %+v", output)
	}
	result.Reviews.Threads[0].Comments = append(result.Reviews.Threads[0].Comments, github.ReviewComment{ID: "C2", Body: "new reply"})
	result.Checks[0].Conclusion = "success"
	output, after := inspectionDelta(result, &before, false, "state.json")
	delta := output.(map[string]any)
	added := delta["added"].(map[string]json.RawMessage)
	changed := delta["changed"].(map[string]json.RawMessage)
	if len(added) != 1 || len(changed) != 1 || added["comment:C2"] == nil || changed["check:check:1"] == nil {
		t.Fatalf("unexpected delta: %+v", output)
	}
	encoded, _ := json.Marshal(output)
	if bytes.Contains(encoded, []byte(`"body":"request"`)) {
		t.Fatal("previous conversation replayed")
	}
	result.Reviews.Threads = nil
	output, _ = inspectionDelta(result, &after, false, "state.json")
	removed := output.(map[string]any)["removed"].([]string)
	if strings.Join(removed, ",") != "comment:C1,comment:C2,thread:T1" {
		t.Fatalf("missing removed entities: %v", removed)
	}
	for _, mode := range []string{"head changed", "partial", "full"} {
		copy := result
		if mode == "head changed" {
			copy.PR.HeadSHA = "new"
		}
		if mode == "partial" {
			copy.Complete = false
		}
		output, _ := inspectionDelta(copy, &before, mode == "full", "state.json")
		if _, ok := output.(github.InspectResult); !ok {
			t.Fatalf("%s did not preserve the full snapshot", mode)
		}
	}
}

func TestInspectionStateRejectsWrongScopeAndConcurrentConsumers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := inspectionItems(stateFixtureResult())
	data, _ := json.Marshal(state)
	if err := atomicJSONFile(path, data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []struct {
		repo   string
		number int
	}{{"another/repo", 7}, {"owner/repo", 8}} {
		if _, err := readInspectionState(path, scope.repo, scope.number); err == nil {
			t.Fatal("accepted foreign state")
		}
	}
	unlock, err := lockInspectionState(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockInspectionState(path); err == nil {
		t.Fatal("accepted concurrent consumer")
	}
	unlock()
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("state lock retained: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readInspectionState(path, "owner/repo", 7); err == nil {
		t.Fatal("accepted malformed state")
	}
}
