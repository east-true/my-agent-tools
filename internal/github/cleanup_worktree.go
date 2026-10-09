package github

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func (client Client) cleanupWorktreeCandidates(ctx context.Context, scope string) (map[string]branchWorktree, map[string]string, error) {
	worktrees, err := client.branchWorktrees(ctx)
	if err != nil {
		return nil, nil, err
	}
	out, err := client.Runner.Run(ctx, nil, "git", "rev-parse", "--show-toplevel")
	if err != nil && !worktrees[0].Bare {
		return nil, nil, err
	}
	current := ""
	if err == nil {
		current = filepath.Clean(strings.TrimSpace(string(out)))
	}
	byBranch, skips := map[string]branchWorktree{}, map[string]string{}
	for index, worktree := range worktrees {
		if worktree.Branch == "" {
			continue
		}
		if _, exists := byBranch[worktree.Branch]; exists {
			skips[worktree.Branch] = "branch is checked out in multiple worktrees"
			continue
		}
		byBranch[worktree.Branch] = worktree
		switch {
		case scope == "remote":
			skips[worktree.Branch] = "worktree retained by remote-only scope"
		case index == 0:
			skips[worktree.Branch] = "main worktree is retained"
		case worktree.Path == current:
			skips[worktree.Branch] = "current worktree is retained"
		case worktree.Locked:
			skips[worktree.Branch] = "worktree is locked"
		case worktree.Prunable:
			skips[worktree.Branch] = "worktree is unavailable"
		default:
			if err := client.cleanBranchWorktree(ctx, worktree); err != nil {
				skips[worktree.Branch] = err.Error()
			}
		}
	}
	return byBranch, skips, nil
}

func (client Client) cleanBranchWorktree(ctx context.Context, worktree branchWorktree) error {
	if err := client.verifyBranchWorktree(ctx, worktree); err != nil {
		return fmt.Errorf("worktree is unavailable or changed: %w", err)
	}
	out, err := client.Runner.Run(ctx, nil, "git", "--no-optional-locks", "-C", worktree.Path, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored")
	if err != nil {
		return err
	}
	if len(out) > 0 {
		return errors.New("worktree has modified, untracked, or ignored files")
	}
	return nil
}

func (client Client) removeCleanupWorktree(ctx context.Context, target CleanupTarget, options CleanupOptions) error {
	worktrees, skips, err := client.cleanupWorktreeCandidates(ctx, options.Scope)
	if err != nil {
		return err
	}
	worktree, exists := worktrees[target.Name]
	if !exists || worktree.Path != target.WorktreePath || skips[target.Name] != "" {
		return errors.New("worktree changed or is no longer eligible; preview again")
	}
	out, err := client.Runner.Run(ctx, nil, "git", "-C", worktree.Path, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != target.SHA {
		return errors.New("worktree commit changed; preview again")
	}
	_, err = client.Runner.Run(ctx, nil, "git", "worktree", "remove", "--", worktree.Path)
	return err
}
