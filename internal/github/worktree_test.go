package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type worktreeFixture struct {
	client Client
	local  string
	path   string
	branch string
	t      *testing.T
}

func (f worktreeFixture) git(args ...string) string {
	f.t.Helper()
	out, err := f.client.Runner.Run(context.Background(), nil, "git", args...)
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func newWorktreeFixture(t *testing.T) worktreeFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := canonicalTempDir(t)
	local := filepath.Join(root, "local with spaces")
	f := worktreeFixture{client: Client{Runner: directoryRunner{dir: root}}, local: local, branch: "17-fix-login", t: t}
	f.path = filepath.Join(local+".worktrees", f.branch)
	remote := filepath.Join(root, "remote.git")
	f.git("init", "--bare", remote)
	f.git("init", local)
	f.client.Runner = directoryRunner{dir: local}
	f.git("symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(local, "tracked.txt"), []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.git("add", "tracked.txt")
	f.git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "initial")
	f.git("remote", "add", "origin", remote)
	f.git("push", "origin", "HEAD:refs/heads/"+f.branch)
	return f
}

func TestWorktreePreservesIndexAndWorkingDirectory(t *testing.T) {
	f := newWorktreeFixture(t)
	tracked := filepath.Join(f.local, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.git("add", "tracked.txt")
	if err := os.WriteFile(tracked, []byte("unstaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := f.git("status", "--porcelain")
	index := f.git("show", ":tracked.txt")
	if _, _, err := f.client.worktreeBranch(context.Background(), f.branch, f.path); err != nil {
		t.Fatal(err)
	}
	if f.git("branch", "--show-current") != "main" || f.git("status", "--porcelain") != before || f.git("show", ":tracked.txt") != index {
		t.Fatal("current branch or staged/unstaged changes were modified")
	}
	if f.git("-C", f.path, "show", "HEAD:tracked.txt") != "initial" {
		t.Fatal("worktree did not start from the remote branch")
	}
}

func TestWorktreeRefusesConflicts(t *testing.T) {
	for _, scenario := range []string{"occupied directory", "wrong upstream", "missing registered worktree"} {
		t.Run(scenario, func(t *testing.T) {
			f := newWorktreeFixture(t)
			switch scenario {
			case "occupied directory":
				if err := os.MkdirAll(f.path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(f.path, "keep.txt"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "wrong upstream":
				f.git("branch", f.branch)
			case "missing registered worktree":
				if _, _, err := f.client.worktreeBranch(context.Background(), f.branch, f.path); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(f.path, f.path+"-moved"); err != nil {
					t.Fatal(err)
				}
			}
			before := f.git("show-ref")
			if _, _, err := f.client.worktreeBranch(context.Background(), f.branch, f.path); err == nil {
				t.Fatal("conflicting worktree was accepted")
			}
			if f.git("show-ref") != before || f.git("branch", "--show-current") != "main" {
				t.Fatal("conflict modified refs or current branch")
			}
			if scenario == "occupied directory" {
				data, err := os.ReadFile(filepath.Join(f.path, "keep.txt"))
				if err != nil || string(data) != "keep" {
					t.Fatal("existing destination was modified")
				}
			}
		})
	}
}

func TestWorktreePreservesExistingTrackingBranchCommits(t *testing.T) {
	f := newWorktreeFixture(t)
	f.git("fetch", "origin")
	f.git("branch", "--track", f.branch, "origin/"+f.branch)
	f.git("switch", f.branch)
	f.git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "local work")
	commit := f.git("rev-parse", "HEAD")
	f.git("switch", "main")
	if _, _, err := f.client.worktreeBranch(context.Background(), f.branch, f.path); err != nil {
		t.Fatal(err)
	}
	if f.git("-C", f.path, "rev-parse", "HEAD") != commit {
		t.Fatal("unpublished local commit was lost")
	}
}

func TestWorktreeReusesCustomPathAndPreservesChanges(t *testing.T) {
	f := newWorktreeFixture(t)
	custom := filepath.Join(canonicalTempDir(t), "custom worktree")
	if _, _, err := f.client.worktreeBranch(context.Background(), f.branch, custom); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(custom, "tracked.txt")
	if err := os.WriteFile(file, []byte("work in progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	actual, reused, err := f.client.worktreeBranch(context.Background(), f.branch, f.path)
	if err != nil || actual != custom || !reused {
		t.Fatal("custom worktree not reused", actual, reused, err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "work in progress" {
		t.Fatal("resuming changed user files", err)
	}
	if _, err := os.Stat(f.path); !os.IsNotExist(err) {
		t.Fatal("resume created another worktree", err)
	}
}

type githubOriginRunner struct{ directoryRunner }

func (r githubOriginRunner) Run(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	if name == "git" && strings.Join(args, " ") == "remote get-url origin" {
		return []byte("https://github.com/owner/repo.git\n"), nil
	}
	return r.directoryRunner.Run(ctx, input, name, args...)
}

func TestBranchCompletionCreatesAndResumesWorktreeFromLinkedCheckout(t *testing.T) {
	w := newWorktreeFixture(t)
	f := newFixture(t, func(out http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/git/ref/heads/main":
			fmt.Fprintf(out, `{"object":{"sha":%q}}`, w.git("rev-parse", "main"))
		case "/repos/owner/repo/git/ref/heads/" + w.branch:
			fmt.Fprintf(out, `{"ref":%q}`, "refs/heads/"+w.branch)
		case "/graphql":
			fmt.Fprintf(out, `{"data":{"node":{"linkedBranches":{"nodes":[{"ref":{"name":%q,"repository":{"nameWithOwner":"owner/repo"}}}],"pageInfo":{"hasNextPage":false}}}}}`, w.branch)
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	})
	f.client.Runner = githubOriginRunner{directoryRunner{dir: w.local}}
	plan := Plan{Repo: "owner/repo", DefaultBranch: "main", WorkType: "fix", Slug: "login"}
	if err := f.client.PrepareBranch(context.Background(), &plan, true); err != nil {
		t.Fatal(err)
	}
	if plan.BranchPlan.WorktreePath != filepath.Join(w.local+".worktrees", "<issue-number>-fix-login") {
		t.Fatalf("wrong planned worktree: %+v", plan.BranchPlan)
	}
	if _, err := os.Stat(w.local + ".worktrees"); !os.IsNotExist(err) || len(f.writes()) != 0 {
		t.Fatal("planning wrote to Git or GitHub")
	}
	for _, repeat := range []bool{false, true} {
		result := Result{Number: 17, IssueNodeID: "I_17"}
		if err := f.client.CompleteBranch(context.Background(), plan, &result); err != nil {
			t.Fatal(err)
		}
		if !result.BranchInfo.CheckedOut || result.BranchInfo.WorktreePath != w.path || result.BranchInfo.WorktreeReused != repeat {
			t.Fatalf("wrong worktree outcome: %+v", result.BranchInfo)
		}
		// Calling from a linked worktree must still resolve the main repository's sibling directory.
		f.client.Runner = githubOriginRunner{directoryRunner{dir: w.path}}
		if err := f.client.PrepareBranch(context.Background(), &plan, true); err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(plan.BranchPlan.WorktreePath) != w.local+".worktrees" {
			t.Fatal("linked checkout changed the default worktree location")
		}
	}
	if len(f.writes()) != 0 {
		t.Fatal("resuming recreated GitHub objects")
	}
}
