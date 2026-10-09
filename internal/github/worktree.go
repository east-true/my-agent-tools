package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type branchWorktree struct {
	Path     string
	Branch   string
	Locked   bool
	Prunable bool
	Bare     bool
}

func (client Client) branchWorktrees(ctx context.Context) ([]branchWorktree, error) {
	out, err := client.Runner.Run(ctx, nil, "git", "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var worktrees []branchWorktree
	for _, field := range strings.Split(string(out), "\x00") {
		if path, ok := strings.CutPrefix(field, "worktree "); ok {
			if !filepath.IsAbs(path) {
				return nil, errors.New("Git returned a non-absolute worktree path")
			}
			worktrees = append(worktrees, branchWorktree{Path: filepath.Clean(path)})
		} else if branch, ok := strings.CutPrefix(field, "branch refs/heads/"); ok {
			if len(worktrees) == 0 {
				return nil, errors.New("Git returned a branch without a worktree path")
			}
			worktrees[len(worktrees)-1].Branch = branch
		} else if len(worktrees) > 0 {
			if field == "bare" {
				worktrees[len(worktrees)-1].Bare = true
			}
			if field == "locked" || strings.HasPrefix(field, "locked ") {
				worktrees[len(worktrees)-1].Locked = true
			}
			if field == "prunable" || strings.HasPrefix(field, "prunable ") {
				worktrees[len(worktrees)-1].Prunable = true
			}
		}
	}
	if len(worktrees) == 0 {
		return nil, errors.New("Git returned no worktrees")
	}
	return worktrees, nil
}

func (client Client) worktreeBranch(ctx context.Context, branch, path string) (string, bool, error) {
	if !filepath.IsAbs(path) {
		return "", false, errors.New("worktree destination must be an absolute path")
	}
	remoteRef := "origin/" + branch
	local, err := client.Runner.Run(ctx, nil, "git", "for-each-ref", "--format=%(refname)%00%(upstream:short)", "refs/heads/"+branch)
	if err != nil {
		return "", false, err
	}
	exists := len(local) > 0
	if exists && strings.TrimSuffix(string(local), "\n") != "refs/heads/"+branch+"\x00"+remoteRef {
		return "", false, errors.New("existing local branch has a different upstream; it was not overwritten")
	}
	worktrees, err := client.branchWorktrees(ctx)
	if err != nil {
		return "", false, err
	}
	for _, worktree := range worktrees {
		if worktree.Branch != branch {
			continue
		}
		if err := client.verifyBranchWorktree(ctx, worktree); err != nil {
			return "", false, fmt.Errorf("existing worktree %s is unavailable or changed: %w", worktree.Path, err)
		}
		return worktree.Path, true, nil
	}
	if _, err := os.Lstat(path); err == nil {
		return "", false, fmt.Errorf("worktree destination %s already exists; it was not changed", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	if _, err := client.Runner.Run(ctx, nil, "git", "fetch", "--no-tags", "origin", "refs/heads/"+branch+":refs/remotes/"+remoteRef); err != nil {
		return "", false, err
	}
	args := []string{"worktree", "add"}
	if exists {
		args = append(args, "--", path, branch)
	} else {
		args = append(args, "--track", "-b", branch, "--", path, remoteRef)
	}
	if _, err := client.Runner.Run(ctx, nil, "git", args...); err != nil {
		return "", false, err
	}
	return path, false, nil
}

func (client Client) verifyBranchWorktree(ctx context.Context, worktree branchWorktree) error {
	// A stale registration must not mistake an unrelated repository for this worktree.
	current, err := client.Runner.Run(ctx, nil, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	other, err := client.Runner.Run(ctx, nil, "git", "-C", worktree.Path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	currentPath, err := filepath.EvalSymlinks(strings.TrimSpace(string(current)))
	if err != nil {
		return err
	}
	otherPath, err := filepath.EvalSymlinks(strings.TrimSpace(string(other)))
	if err != nil {
		return err
	}
	if currentPath != otherPath {
		return errors.New("worktree belongs to a different repository")
	}
	head, err := client.Runner.Run(ctx, nil, "git", "-C", worktree.Path, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(head)) != "refs/heads/"+worktree.Branch {
		return errors.New("worktree has a different checked-out branch")
	}
	return nil
}
