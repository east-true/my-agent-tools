package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func finishedCleanupWorktree(t *testing.T) (*cleanupWorld, string, string) {
	t.Helper()
	w := newCleanupWorld(t)
	name := "17-fix-login"
	w.branch(name, true, true)
	w.issues[17] = "closed"
	path := filepath.Join(w.runner.dir+".worktrees", name)
	w.git("worktree", "add", path, name)
	return w, name, path
}

func TestCleanupRemovesFinishedWorktreesAccordingToScope(t *testing.T) {
	for _, scope := range []string{"both", "local", "remote"} {
		t.Run(scope, func(t *testing.T) {
			w, name, path := finishedCleanupWorktree(t)
			plan := w.plan(scope)
			if scope != "remote" {
				target := cleanupTarget(t, plan, "worktree", name)
				if !target.Eligible || target.WorktreePath != path {
					t.Fatal("clean worktree not planned", target)
				}
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("preview removed worktree", err)
			}
			result, err := w.client.ApplyCleanup(context.Background(), plan)
			if err != nil {
				t.Fatal(result, err)
			}
			if scope == "remote" {
				if _, err := os.Stat(path); err != nil {
					t.Fatal("remote-only cleanup removed local worktree")
				}
				w.git("show-ref", "--verify", "refs/heads/"+name)
			} else {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("finished worktree retained", err)
				}
				if _, err := w.runner.directoryRunner.Run(context.Background(), nil, "git", "show-ref", "--verify", "refs/heads/"+name); err == nil {
					t.Fatal("finished local branch retained")
				}
			}
			_, remoteErr := w.remoteRunner.Run(context.Background(), nil, "git", "show-ref", "--verify", "refs/heads/"+name)
			if (scope == "both") != (remoteErr != nil) {
				t.Fatal("remote scope was not respected", scope, remoteErr)
			}
		})
	}
}

func TestCleanupRetainsUnsafeWorktreesAndTheirBranches(t *testing.T) {
	for _, scenario := range []string{"untracked", "staged", "ignored", "locked", "current", "unpublished", "open PR", "protected"} {
		t.Run(scenario, func(t *testing.T) {
			w, name, path := finishedCleanupWorktree(t)
			switch scenario {
			case "untracked", "staged", "ignored":
				if scenario == "ignored" {
					exclude := w.git("rev-parse", "--git-path", "info/exclude")
					if !filepath.IsAbs(exclude) {
						exclude = filepath.Join(w.runner.dir, exclude)
					}
					if err := os.WriteFile(exclude, []byte("keep.txt\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(path, "keep.txt"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
				if scenario == "staged" {
					w.git("-C", path, "add", "keep.txt")
				}
			case "locked":
				w.git("worktree", "lock", path)
			case "current":
				w.runner.dir = path
			case "unpublished":
				w.git("-C", path, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "unpublished")
			case "open PR":
				w.pr(1, name, "open", false)
			case "protected":
				w.protected[name] = true
			}
			plan := w.plan("both")
			for _, scope := range []string{"worktree", "local", "remote"} {
				if target := cleanupTarget(t, plan, scope, name); target.Eligible {
					t.Fatal("unsafe worktree branch eligible", target)
				}
			}
			if result, err := w.client.ApplyCleanup(context.Background(), plan); err != nil {
				t.Fatal(result, err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("unsafe worktree removed", err)
			}
			w.git("show-ref", "--verify", "refs/heads/"+name)
			if _, err := w.remoteRunner.Run(context.Background(), nil, "git", "show-ref", "--verify", "refs/heads/"+name); err != nil {
				t.Fatal("unsafe worktree remote deleted", err)
			}
		})
	}
}

func TestCleanupWorktreeChangesAfterPreviewKeepBranches(t *testing.T) {
	for _, scenario := range []string{"dirty", "moved", "locked", "advanced"} {
		t.Run(scenario, func(t *testing.T) {
			w, name, path := finishedCleanupWorktree(t)
			plan := w.plan("both")
			switch scenario {
			case "dirty":
				if err := os.WriteFile(filepath.Join(path, "keep.txt"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "moved":
				w.git("worktree", "move", path, path+"-moved")
				path += "-moved"
			case "locked":
				w.git("worktree", "lock", path)
			case "advanced":
				w.git("-C", path, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "new work")
			}
			result, err := w.client.ApplyCleanup(context.Background(), plan)
			if err != nil {
				t.Fatal(result, err)
			}
			for _, action := range result.Actions {
				if action.Status == "deleted" {
					t.Fatal("changed worktree or branch deleted", result)
				}
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("changed worktree removed", err)
			}
			w.git("show-ref", "--verify", "refs/heads/"+name)
		})
	}
}

func TestCleanupFailedWorktreeRemovalKeepsRefs(t *testing.T) {
	w, name, path := finishedCleanupWorktree(t)
	plan := w.plan("both")
	w.runner.before = func(args []string) error {
		if len(args) > 1 && args[0] == "worktree" && args[1] == "remove" {
			return errors.New("worktree removal refused")
		}
		return nil
	}
	result, err := w.client.ApplyCleanup(context.Background(), plan)
	if err == nil || result.Status != "partial" {
		t.Fatal("worktree removal failure hidden", result, err)
	}
	for _, action := range result.Actions {
		if action.Status == "deleted" {
			t.Fatal("branch deleted after worktree failure", result)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed worktree disappeared", err)
	}
	w.git("show-ref", "--verify", "refs/heads/"+name)
	for _, args := range w.runner.commands {
		if strings.Contains(strings.Join(args, " "), "--force") && len(args) > 1 && args[0] == "worktree" {
			t.Fatal("worktree removal was forced", args)
		}
	}
}

func TestCleanupWorktreesFromBareRepository(t *testing.T) {
	w := newCleanupWorld(t)
	name := "17-fix-login"
	w.branch(name, true, true)
	w.issues[17] = "closed"
	bare := filepath.Join(t.TempDir(), "checkout.git")
	w.git("clone", "--bare", w.runner.dir, bare)
	w.runner.dir = bare
	w.git("remote", "set-url", "origin", "https://github.com/owner/repo.git")
	w.git("config", "branch."+name+".remote", "origin")
	w.git("config", "branch."+name+".merge", "refs/heads/"+name)
	path := filepath.Join(t.TempDir(), "worktree")
	w.git("worktree", "add", path, name)
	plan := w.plan("local")
	if !cleanupTarget(t, plan, "worktree", name).Eligible {
		t.Fatal("bare repository could not plan worktree cleanup", plan)
	}
	result, err := w.client.ApplyCleanup(context.Background(), plan)
	if err != nil {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("bare repository worktree retained", err)
	}
}
