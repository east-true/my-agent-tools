package github

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinkedWorktreesShareStateOutsideGitMetadata(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "project")
	gitDir := filepath.Join(main, ".git", "worktrees", "task")
	worktree := filepath.Join(root, "task")
	if err := os.MkdirAll(gitDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktree, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+gitDir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(worktree, "nested"))
	if actual := defaultStateDirectory(); actual != filepath.Join(main, ".tools", "state") {
		t.Fatalf("state=%s", actual)
	}
}
