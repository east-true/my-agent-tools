package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, *scope) {
	t.Helper()
	root := t.TempDir()
	s, err := openScope(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.root.Close() })
	return root, s
}
func put(t *testing.T, root, name, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0640); err != nil {
		t.Fatal(err)
	}
}
func TestInspectCombinesPatternsRangesAndOriginalByteHashes(t *testing.T) {
	root, _ := fixture(t)
	text := "head\r\ncache one\r\n한국어\r\nretry two\r\ntail\r\n"
	put(t, root, "src/a.go", text)
	put(t, root, "dist/ignored.go", "cache ignored")
	put(t, root, "src/b.go", "no match\n")
	result, err := Inspect(context.Background(), InspectOptions{Options: Options{Root: root, Include: []string{"**/*.go"}}, Patterns: []string{"cache", "retry"}, Context: 1, Hash: true})
	if err != nil || !result.Complete || len(result.Files) != 1 {
		t.Fatal(result, err)
	}
	item := result.Files[0]
	if item.SHA256 != digest([]byte(text)) || !reflect.DeepEqual(item.Matches, []int{2, 4}) || len(item.Ranges) != 1 || item.Ranges[0].Start != 1 || len(item.Ranges[0].Lines) != 5 {
		t.Fatal(item)
	}
	lines, err := Inspect(context.Background(), InspectOptions{Options: Options{Root: root, Paths: []string{"src/a.go"}}, Start: 2, End: 3})
	if err != nil || !reflect.DeepEqual(lines.Files[0].Ranges[0].Lines, []string{"cache one", "한국어"}) {
		t.Fatal(lines, err)
	}
}
func TestInspectDoesNotReadUnselectedOrBinaryContentsAndReportsBounds(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "binary.dat", "\x00\xff")
	put(t, root, "a.txt", strings.Repeat("line\n", 400))
	put(t, root, "b.txt", "line\n")
	meta, err := Inspect(context.Background(), InspectOptions{Options: Options{Root: root, Paths: []string{"binary.dat"}}})
	if err != nil || !meta.Complete || len(meta.Files) != 1 {
		t.Fatal(meta, err)
	}
	for _, options := range []InspectOptions{{Options: Options{Root: root, Paths: []string{"binary.dat"}}, Patterns: []string{"x"}}, {Options: Options{Root: root, Include: []string{"*.txt"}, MaxFiles: 1}}, {Options: Options{Root: root, Paths: []string{"a.txt"}, MaxFileBytes: 10}, Patterns: []string{"line"}}, {Options: Options{Root: root, Paths: []string{"a.txt"}, MaxOutputBytes: 256}, Patterns: []string{"line"}}} {
		r, err := Inspect(context.Background(), options)
		if err != nil || r.Complete {
			t.Fatal(r, err)
		}
	}
}
func TestRootEscapeAndSymbolicLinksAreRejected(t *testing.T) {
	root, s := fixture(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside.txt", "/etc/passwd", "a/../../b", "a\\b", ".tools-fs-apply.lock"} {
		if _, err := cleanPath(name); err == nil {
			t.Fatal(name)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skip("symlink unavailable:", err)
	}
	if _, _, err := s.read("link.txt"); err == nil {
		t.Fatal("followed symlink")
	}
	r, err := Inspect(context.Background(), InspectOptions{Options: Options{Root: root, Paths: []string{"link.txt"}}})
	if err != nil || r.Complete {
		t.Fatal(r, err)
	}
}
func TestDeltaDetectsSameSizeSameTimestampEditsAndKeepsBaselineOnPartialRead(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", "one\ntwo\nthree\n")
	put(t, root, "b.txt", "keep\n")
	put(t, root, "gone.txt", "gone\n")
	state := filepath.Join(root, "snapshot.json")
	options := DeltaOptions{Options: Options{Root: root, Include: []string{"*.txt"}}, StateFile: state, Content: true, Context: 0}
	first, err := Delta(context.Background(), options)
	if err != nil || first.Status != "initialized" || first.Tracked != 3 || len(first.Changes) != 0 {
		t.Fatal(first, err)
	}
	info, _ := os.Stat(filepath.Join(root, "a.txt"))
	put(t, root, "a.txt", "one\nTWO\nthree\n")
	os.Chtimes(filepath.Join(root, "a.txt"), info.ModTime(), info.ModTime())
	os.Remove(filepath.Join(root, "gone.txt"))
	put(t, root, "new.txt", "new\n")
	next, err := Delta(context.Background(), options)
	if err != nil || !next.Complete || len(next.Changes) != 3 || next.Unchanged != 1 {
		t.Fatal(next, err)
	}
	if next.Changes[0].Kind != "modified" || next.Changes[0].Ranges[0].Start != 2 || next.Changes[0].Ranges[0].Lines[0] != "TWO" {
		t.Fatal(next)
	}
	same, err := Delta(context.Background(), options)
	if err != nil || same.Status != "unchanged" || same.StateUpdated {
		t.Fatal(same, err)
	}
	before, _ := os.ReadFile(state)
	put(t, root, "b.txt", strings.Repeat("x", 2<<20))
	partial, err := Delta(context.Background(), options)
	after, _ := os.ReadFile(state)
	if err != nil || partial.Complete || partial.StateUpdated || string(before) != string(after) {
		t.Fatal(partial, err)
	}
}
func TestDeltaRejectsStateScopeCorruptionAndOutputOverflow(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", "a\n")
	state := filepath.Join(root, ".tools/state/fs.json")
	options := DeltaOptions{Options: Options{Root: root}, StateFile: state, Content: true}
	if _, err := Delta(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	mismatch := options
	mismatch.Include = []string{"*.go"}
	if _, err := Delta(context.Background(), mismatch); err == nil {
		t.Fatal("accepted changed scope")
	}
	before, _ := os.ReadFile(state)
	put(t, root, "a.txt", strings.Repeat("changed\n", 100))
	options.MaxOutputBytes = 256
	result, err := Delta(context.Background(), options)
	after, _ := os.ReadFile(state)
	if err != nil || result.Complete || string(before) != string(after) {
		t.Fatal(result, err)
	}
	os.WriteFile(state, []byte("broken"), 0600)
	if _, err := Delta(context.Background(), options); err == nil {
		t.Fatal("accepted corrupt baseline")
	}
}
func TestApplyValidatesEntireBatchPreservesCRLFAndPreviewMakesNoWrites(t *testing.T) {
	root, _ := fixture(t)
	put(t, root, "a.txt", "one\r\n한국어\r\n")
	put(t, root, "b.txt", "other\n")
	newText := "created\n"
	plan := Plan{Version: 1, Files: []Edit{{Path: "a.txt", SHA256: digest([]byte("one\r\n한국어\r\n")), Replacements: []Replacement{{Old: "one", New: "two", Count: 1}}}, {Path: "new.txt", SHA256: "absent", Content: &newText}}}
	preview, err := Apply(context.Background(), Options{Root: root}, plan, false)
	if err != nil || preview.Status != "planned" || preview.Applied != 0 {
		t.Fatal(preview, err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("preview created file")
	}
	blocked := plan
	blocked.Files = append(append([]Edit{}, plan.Files...), Edit{Path: "b.txt", SHA256: strings.Repeat("0", 64), Content: &newText})
	r, err := Apply(context.Background(), Options{Root: root}, blocked, true)
	if err != nil || r.Status != "blocked" || r.Applied != 0 {
		t.Fatal(r, err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "one\r\n한국어\r\n" {
		t.Fatal("modified before validating all files")
	}
	applied, err := Apply(context.Background(), Options{Root: root}, plan, true)
	if err != nil || !applied.Complete || applied.Applied != 2 {
		t.Fatal(applied, err)
	}
	data, _ = os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "two\r\n한국어\r\n" {
		t.Fatal(string(data))
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tools-fs-") {
			t.Fatal("temporary/lock leaked", e.Name())
		}
	}
}
func TestApplyRefusesWrongCountsExistingCreatesAndChangesBeforeCommit(t *testing.T) {
	root, s := fixture(t)
	put(t, root, "a.txt", "same same\n")
	text := "new\n"
	for _, edit := range []Edit{{Path: "a.txt", SHA256: digest([]byte("same same\n")), Replacements: []Replacement{{Old: "same", New: "new", Count: 1}}}, {Path: "a.txt", SHA256: "absent", Content: &text}} {
		r, err := Apply(context.Background(), Options{Root: root}, Plan{Version: 1, Files: []Edit{edit}}, true)
		if err != nil || r.Complete {
			t.Fatal(r, err)
		}
	}
	item, err := s.prepare(Edit{Path: "a.txt", SHA256: digest([]byte("same same\n")), Content: &text})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.stage(&item); err != nil {
		t.Fatal(err)
	}
	defer s.root.Remove(item.temporary)
	put(t, root, "a.txt", "concurrent user work\n")
	if err := s.commit(item); err == nil {
		t.Fatal("overwrote concurrent user edit")
	}
	data, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(data) != "concurrent user work\n" {
		t.Fatal(string(data))
	}
	raw, _ := json.Marshal(Plan{Version: 1, Files: []Edit{item.edit}})
	if len(raw) == 0 {
		t.Fatal("plan not serializable")
	}
}
