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
	"github.com/east-true/my-agent-tools/internal/filesystem"
	"github.com/east-true/my-agent-tools/internal/github"
)

func TestCompactPreservesBundledSourceTextAndLines(t *testing.T) {
	text := strings.Repeat("한글\r\n", 800) + "last"
	lines := make([]any, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 2000)
	}
	value := map[string]any{"source_files": map[string]any{"complete": true, "files": []any{map[string]any{"ranges": []any{map[string]any{"text": text, "lines": lines}}}}}, "summary": strings.Repeat("s", 2000)}
	projection := compactProjection(value).(map[string]any)
	original, _ := json.Marshal(value["source_files"])
	actual, _ := json.Marshal(projection["source_files"])
	if !bytes.Equal(original, actual) || projection["summary_truncated"] != true {
		t.Fatal("source was shortened or unrelated compacting stopped")
	}
}

func TestUnchangedInspectionRetainsRawSubmittedHistoryWithoutInferringObligations(t *testing.T) {
	for _, state := range []string{"CHANGES_REQUESTED", "APPROVED", "DISMISSED"} {
		result := github.InspectResult{Status: "ready", Complete: true, Repo: "owner/repo", Number: 7, PR: github.PRState{HeadSHA: "current"}, Reviews: &github.ReviewResult{Reviews: []github.PRReview{{ID: 1, State: state, Body: "exact request\r\n한국어", CommitID: "old", URL: "https://example.test/review"}}}}
		before := inspectionItems(result)
		value, _ := inspectionDelta(result, &before, false, "state.json")
		object := value.(map[string]any)
		work := object["outstanding"].(*inspectionWork)
		if object["status"] != "unchanged" || object["attention_required"] != false || len(work.Reviews) != 1 || work.Reviews[0].Body != result.Reviews.Reviews[0].Body || len(work.Reasons) != 0 {
			t.Fatal("raw history missing or treated as a current obligation", value)
		}
	}
}

func TestSetupReadBackProofAndMismatchKeepSavedFact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte("{\"github\":{}}\n")
	if err := os.WriteFile(path, data, 0640); err != nil {
		t.Fatal(err)
	}
	proof, err := verifySetupConfig(path, data, data, true, 0640)
	if err != nil || !proof.Verified || proof.Changed || !proof.ModePreserved || proof.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatal(proof, err)
	}
	proof, err = verifySetupConfig(path, []byte("{}"), data, true, 0640)
	if err == nil || proof.Verified || !proof.Saved || proof.Error == "" {
		t.Fatal("post-save failure erased save receipt", proof, err)
	}
}

func invokeFS(t *testing.T, args ...string) (map[string]any, int) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := runFilesystem(context.Background(), args, nil, &out, &stderr)
	var value map[string]any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatal(code, out.String(), stderr.String(), err)
	}
	return value, code
}

func savedReportArgs(t *testing.T, value map[string]any, kind string) []string {
	t.Helper()
	proof, ok := value["saved_report"].(map[string]any)
	if !ok || proof["verified"] != true {
		t.Fatal("full overflow report was not verified", value)
	}
	return []string{kind, "--read-report", proof["path"].(string), "--report-sha256", proof["sha256"].(string), "--json"}
}

func TestLargeJUnitRecoveryIsBoundToOriginalReports(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "TEST-case.xml")
	details := strings.Repeat("unrelated diagnostic\n", 5000) + "target final diagnosis\n"
	os.WriteFile(path, []byte(`<testsuite name="suite" tests="1" failures="1"><testcase name="case"><failure message="failed">`+details+`</failure></testcase></testsuite>`), 0640)
	value, code := invokeFS(t, "test-results", "--root", root, "--json")
	if code != 1 || value["tests"] != float64(1) || value["execution_verified"] != false {
		t.Fatal(code, value)
	}
	args := append(savedReportArgs(t, value, "test-results"), "--pattern", "target final diagnosis")
	recovered, code := invokeFS(t, args...)
	data, _ := json.Marshal(recovered)
	if code != 0 || !bytes.Contains(data, []byte("target final diagnosis")) || recovered["current_files_verified"] != true || recovered["execution_verified"] != false {
		t.Fatal(code, recovered)
	}
	os.WriteFile(path, []byte(`<testsuite tests="0"/>`), 0640)
	if _, code := invokeFS(t, args...); code == 0 {
		t.Fatal("new diagnostics mixed with old counts")
	}
}

func TestLargeDeltaKeepsMetadataPreimageAndDoesNotAdvanceBaseline(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "deleted.txt")
	original := strings.Repeat("old content\n", 8000) + "deleted target\n"
	os.WriteFile(path, []byte(original), 0640)
	state := filepath.Join(root, ".tools", "state.json")
	if _, code := invokeFS(t, "delta", "--root", root, "--state-file", state, "--include-content", "--json"); code != 0 {
		t.Fatal("baseline initialization failed")
	}
	baseline, _ := os.ReadFile(state)
	os.Remove(path)
	value, code := invokeFS(t, "delta", "--root", root, "--state-file", state, "--include-content", "--json")
	after, _ := os.ReadFile(state)
	if code != 1 || !bytes.Equal(baseline, after) || len(value["changes"].([]any)) != 1 {
		t.Fatal("metadata lost or baseline advanced", code, value)
	}
	args := append(savedReportArgs(t, value, "delta"), "--pattern", "deleted target")
	recovered, code := invokeFS(t, args...)
	data, _ := json.Marshal(recovered)
	if code != 0 || recovered["state_updated"] != false || !bytes.Contains(data, []byte("deleted target")) {
		t.Fatal("deleted preimage cannot be recovered", code, recovered)
	}
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("later"), 0600)
	if _, code := invokeFS(t, args...); code == 0 {
		t.Fatal("changed selection accepted")
	}
}

func TestLargeApplyReportRecoveryNeverWritesAndKeepsHistoricalProof(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	os.WriteFile(path, []byte("before\r\n"), 0640)
	newText := strings.Repeat("new content\n", 8000) + "target final edit\n"
	plan := filesystem.Plan{Version: 1, Files: []filesystem.Edit{{Path: "a.txt", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("before\r\n"))), Content: &newText}}}
	data, _ := json.Marshal(plan)
	planPath := filepath.Join(t.TempDir(), "plan.json")
	os.WriteFile(planPath, data, 0600)
	value, code := invokeFS(t, "apply", "--root", root, "--plan", planPath, "--apply", "--report-changes", "--json")
	if code != 0 || value["applied"] != float64(1) || value["report_complete"] != false {
		t.Fatal(code, value)
	}
	args := append(savedReportArgs(t, value, "apply"), "--pattern", "target final edit")
	os.WriteFile(path, []byte("later edit"), 0640)
	recovered, code := invokeFS(t, args...)
	actual, _ := os.ReadFile(path)
	data, _ = json.Marshal(recovered)
	if code != 0 || string(actual) != "later edit" || recovered["current_files_verified"] != false || !bytes.Contains(data, []byte("target final edit")) {
		t.Fatal("recovery reapplied plan or claimed fresh state", code, recovered)
	}
	if _, code := invokeFS(t, append(args, "--apply")...); code == 0 {
		t.Fatal("read-report accepted mutation flags")
	}
}

func TestReviewSourcesUseUnresolvedThreadPathsAndKeepRawLocalBytes(t *testing.T) {
	root := t.TempDir()
	text := "local source\r\n한국어"
	os.WriteFile(filepath.Join(root, "a.txt"), []byte(text), 0640)
	options := reviewSources{Root: root, Threads: true}
	reviews := &github.ReviewResult{Threads: []github.ReviewThread{{Path: "a.txt"}, {Path: "a.txt", Outdated: true}, {Path: "skip.txt", Resolved: true}}}
	value, err := options.read(context.Background(), reviews)
	if err != nil || !value.Complete || len(value.Files) != 1 || *value.Files[0].Ranges[0].Text != text {
		t.Fatal(value, err)
	}
	reviews.Threads = append(reviews.Threads, github.ReviewThread{Path: "missing.txt", Outdated: true})
	value, err = options.read(context.Background(), reviews)
	if err != nil || value.Complete || len(value.Problems) == 0 {
		t.Fatal("missing outdated path silently mapped", value, err)
	}
}

func TestOfflineCIReadWorksAcrossChainsWithoutAuthenticationOrMutation(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("plain log %d", i+1)
	}
	lines[70] = "hidden decision parameter=wrong"
	occurrence := github.CIOccurrence{JobID: 3, StartLine: 11, EndLine: 110}
	failure := github.CIFailureResult{Repo: "owner/repo", Run: github.CIRun{ID: 2, Attempt: 4, HeadSHA: "head"}, Complete: false, Evidence: []github.CIEvidence{{Kind: "log", Truncated: true, Lines: lines, Occurrences: []github.CIOccurrence{occurrence}}}}
	projected := compactProjection(map[string]any{"lines": func() []any {
		values := make([]any, len(lines))
		for i, line := range lines {
			values[i] = line
		}
		return values
	}()})
	data, _ := json.Marshal(projected)
	if bytes.Contains(data, []byte("hidden decision")) {
		t.Fatal("test did not reproduce missing non-error context")
	}
	for _, chain := range [][]string{{"ci", "failures"}, {"ci", "rerun"}, {"pr", "inspect"}, {"pr", "submit"}, {"pr", "merge"}} {
		path := filepath.Join(t.TempDir(), "evidence.json")
		data, _ := json.Marshal(map[string]any{"inspection": map[string]any{"failures": []github.CIFailureResult{failure}}})
		os.WriteFile(path, data, 0600)
		args := append([]string{"github"}, chain...)
		args = append(args, "--read-evidence", path, "--evidence-sha256", fmt.Sprintf("%x", sha256.Sum256(data)), "--evidence-run", "2", "--evidence-attempt", "4", "--job", "3", "--range", "81:81", "--json")
		var out, stderr bytes.Buffer
		called := false
		factory := func(context.Context, command.Runner) (github.API, error) {
			called = true
			return nil, nil
		}
		code := run(context.Background(), args, nil, &out, &stderr, fakeRunner{}, factory)
		if code != 0 || called || !strings.Contains(out.String(), "hidden decision parameter=wrong") || !strings.Contains(out.String(), `"collection_truncated":true`) || !strings.Contains(out.String(), `"fresh_state_verified":false`) {
			t.Fatal(chain, code, called, out.String(), stderr.String())
		}
		out.Reset()
		code = run(context.Background(), append(args, "--repo", "owner/repo"), nil, &out, &stderr, fakeRunner{}, factory)
		if code == 0 || called {
			t.Fatal("offline read accepted remote operation flags", chain)
		}
	}
}

func TestAutomaticReportStorageCannotEscapeRootThroughSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".tools")); err != nil {
		t.Skip(err)
	}
	proof := saveFSReport(filepath.Join(root, ".tools", "state", "report.json"), fsReportArchive{Version: 1, Kind: "apply", Payload: json.RawMessage(`{"status":"applied"}`)}, root)
	files, err := os.ReadDir(outside)
	if proof.Verified || proof.Error == "" || err != nil || len(files) != 0 {
		t.Fatal("automatic artifact escaped root", proof, files, err)
	}
}

func TestRequestedLargeReportContextFinishesInFirstResponse(t *testing.T) {
	root := t.TempDir()
	detail := strings.Repeat("unrelated\n", 9000) + "target diagnosis\n"
	os.WriteFile(filepath.Join(root, "TEST-case.xml"), []byte(`<testsuite tests="1" failures="1"><testcase name="case"><failure>`+detail+`</failure></testcase></testsuite>`), 0600)
	junit, code := invokeFS(t, "test-results", "--root", root, "--diagnostic-pattern", "target diagnosis", "--json")
	if code != 0 || junit["saved_report"] != nil || junit["tests"] != float64(1) || junit["diagnostics_selected"] != true {
		t.Fatal("diagnostic required a follow-up", code, junit)
	}
	old := strings.ReplaceAll(detail, "\n", "\r\n")
	os.WriteFile(filepath.Join(root, "old.txt"), []byte(old), 0640)
	state := filepath.Join(root, ".tools", "baseline.json")
	invokeFS(t, "delta", "--root", root, "--state-file", state, "--path", "old.txt", "--include-content", "--json")
	os.Remove(filepath.Join(root, "old.txt"))
	delta, code := invokeFS(t, "delta", "--root", root, "--state-file", state, "--path", "old.txt", "--include-content", "--report-pattern", "target diagnosis", "--peek", "--json")
	if code != 0 || delta["saved_report"] != nil || delta["report_selected"] != true {
		t.Fatal("delta required a follow-up", code, delta)
	}
	before := delta["changes"].([]any)[0].(map[string]any)["before_raw_ranges"].([]any)[0].(map[string]any)
	if before["text"] != "target diagnosis\r\n" || before["start"] != float64(9001) {
		t.Fatal("old raw context changed", before)
	}
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("before"), 0640)
	newText := strings.Repeat("new content\n", 8000) + "target final edit\n"
	plan := filesystem.Plan{Version: 1, Files: []filesystem.Edit{{Path: "a.txt", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("before"))), Content: &newText}}}
	data, _ := json.Marshal(plan)
	planPath := filepath.Join(t.TempDir(), "plan.json")
	os.WriteFile(planPath, data, 0600)
	apply, code := invokeFS(t, "apply", "--root", root, "--plan", planPath, "--apply", "--report-changes", "--report-pattern", "target final edit", "--json")
	file := apply["files"].([]any)[0].(map[string]any)
	if code != 0 || apply["saved_report"] != nil || apply["report_complete"] != true || file["mode_preserved"] != true || file["before_mode"] != float64(0640) || file["mode"] != float64(0640) {
		t.Fatal("apply needed report/permission requery", code, apply)
	}
}
