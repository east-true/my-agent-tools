package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSavedDeletedPathAllowsDeletedParentsButRejectsReplacedAncestors(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{"removed/file.txt": "absent"}
	if err := VerifyReportFiles(context.Background(), Options{Root: root, Paths: []string{"removed/file.txt"}}, files, true); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "removed"), []byte("replaced ancestor"), 0600)
	if err := VerifyReportFiles(context.Background(), Options{Root: root, Paths: []string{"removed/file.txt"}}, files, true); err == nil {
		t.Fatal("replaced ancestor accepted")
	}
}

func TestExplicitDeltaPathCanRemainAbsentAcrossBaselineAndPeek(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, ".tools", "baseline.json")
	options := DeltaOptions{Options: Options{Root: root, Paths: []string{"future.txt"}}, StateFile: state, Content: true}
	initial, err := Delta(context.Background(), options)
	if err != nil || !initial.Complete || initial.Status != "initialized" {
		t.Fatal(initial, err)
	}
	os.WriteFile(filepath.Join(root, "future.txt"), []byte("added"), 0640)
	added, err := Delta(context.Background(), options)
	if err != nil || !added.Complete || added.Changes[0].Kind != "added" {
		t.Fatal(added, err)
	}
	os.Remove(filepath.Join(root, "future.txt"))
	deleted, err := Delta(context.Background(), options)
	if err != nil || !deleted.Complete || deleted.Changes[0].Kind != "deleted" {
		t.Fatal(deleted, err)
	}
	options.Peek = true
	unchanged, err := Delta(context.Background(), options)
	if err != nil || !unchanged.Complete || unchanged.Status != "unchanged" || unchanged.BaselinePreserved == nil || !*unchanged.BaselinePreserved || unchanged.BaselineSHA256 == "" {
		t.Fatal(unchanged, err)
	}
}
