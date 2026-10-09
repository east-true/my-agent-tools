package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type worktreeCLIRunner struct {
	root   string
	writes int
	args   []string
}

func (r *worktreeCLIRunner) Run(_ context.Context, _ []byte, _ string, args ...string) ([]byte, error) {
	switch strings.Join(args, " ") {
	case "rev-parse --show-toplevel":
		return []byte(r.root + "\n"), nil
	case "rev-parse --is-inside-work-tree":
		return []byte("true\n"), nil
	case "remote get-url origin":
		return []byte("https://github.com/owner/repo.git\n"), nil
	case "worktree list --porcelain -z":
		return []byte("worktree " + r.root + "\x00branch refs/heads/main\x00\x00"), nil
	}
	if args[0] == "for-each-ref" {
		return nil, nil
	}
	if args[0] == "fetch" || (len(args) > 1 && args[0] == "worktree" && args[1] == "add") {
		r.writes++
		r.args = append([]string(nil), args...)
		return nil, nil
	}
	return nil, errors.New("unexpected command: " + strings.Join(args, " "))
}

func TestIssueCommandsUseWorktreesAndRespectSkipFlags(t *testing.T) {
	for _, action := range []string{"create", "branch"} {
		for _, mode := range []string{"create worktree", "dry-run", "no-checkout", "no-branch"} {
			if action == "branch" && mode == "no-branch" {
				continue
			}
			t.Run(action+"/"+mode, func(t *testing.T) {
				r := &worktreeCLIRunner{root: t.TempDir()}
				api := &fakeAPI{}
				args := []string{"github", "issue", action, "--json"}
				if action == "create" {
					args = append(args, "--file", "-")
				} else {
					args = append(args, "--number", "1")
				}
				if mode != "create worktree" {
					args = append(args, "--"+mode)
				}
				var out, stderr bytes.Buffer
				code := run(context.Background(), args, strings.NewReader(`{"title":"feat: add cli","body":"기능 추가"}`), &out, &stderr, r,
					func(context.Context, command.Runner) (github.API, error) { return api, nil })
				if code != 0 {
					t.Fatal(code, out.String(), stderr.String())
				}
				if mode == "create worktree" {
					var result github.Result
					if err := json.Unmarshal(out.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(r.root+".worktrees", "1-feat-add-cli")
					if r.writes != 2 || result.BranchInfo == nil || !result.BranchInfo.CheckedOut || result.BranchInfo.WorktreePath != path {
						t.Fatal("CLI did not create worktree", out.String(), r)
					}
					if strings.Join(r.args, "\x00") != strings.Join([]string{"worktree", "add", "--track", "-b", "1-feat-add-cli", "--", path, "origin/1-feat-add-cli"}, "\x00") {
						t.Fatal("wrong worktree command", r.args)
					}
				} else if r.writes != 0 {
					t.Fatal("skip flag mutated Git", r)
				}
				if mode == "dry-run" {
					var result struct {
						Plan github.Plan `json:"plan"`
					}
					if err := json.Unmarshal(out.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					name := "<issue-number>-feat-add-cli"
					if action == "branch" {
						name = "1-feat-add-cli"
					}
					if api.writes != 0 || result.Plan.BranchPlan.WorktreePath != filepath.Join(r.root+".worktrees", name) {
						t.Fatal("incorrect dry-run", out.String())
					}
				}
			})
		}
	}
}
