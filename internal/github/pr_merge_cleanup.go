package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// MergePullRequest keeps checking, waiting, merging and optional exact-branch
// cleanup in one operation. Cleanup never follows acceptance alone.
func (client Client) MergePullRequest(ctx context.Context, repo string, options MergeOptions) (MergeResult, error) {
	result := MergeResult{Status: "error", Repo: repo, Number: options.Number}
	if err := options.Normalize(); err != nil {
		return result, err
	}
	if !options.Cleanup {
		return client.mergePullRequest(ctx, repo, options)
	}
	var source cleanupPR
	if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/pulls/%d", repo, options.Number), nil, &source); err != nil {
		result.Error = err.Error()
		return result, err
	}
	if source.Number != options.Number || source.Head.Ref == "" || source.Head.SHA == "" {
		return result, errors.New("PR cleanup source metadata is incomplete")
	}
	result, err := client.mergePullRequest(ctx, repo, options)
	if err != nil || result.Status != "merged" {
		return result, err
	}
	result.Merged = true
	if source.Head.Repo == nil || !strings.EqualFold(source.Head.Repo.Name, repo) {
		result.Notes = append(result.Notes, "Fork/deleted source repository: cleanup skipped.")
		return result, nil
	}
	fail := func(err error) (MergeResult, error) {
		result.Status, result.Error = "partial", err.Error()
		return result, err
	}
	if result.HeadSHA != "" && result.HeadSHA != source.Head.SHA {
		return fail(errors.New("PR head changed before merge; cleanup skipped for the earlier branch snapshot"))
	}
	plan, err := client.PlanCleanup(ctx, repo, CleanupOptions{Remote: options.Remote, Scope: options.Scope, Branch: source.Head.Ref})
	if err != nil {
		return fail(err)
	}
	for i := range plan.Targets {
		if plan.Targets[i].SHA != source.Head.SHA {
			plan.Targets[i].Eligible, plan.Targets[i].Skip = false, "branch commit differs from the merged PR; preserved"
		}
	}
	cleanup, err := client.ApplyCleanup(ctx, plan)
	result.Cleanup = &cleanup
	if err != nil {
		return fail(err)
	}
	for _, action := range cleanup.Actions {
		if action.Status != "deleted" {
			result.Status = "partial"
			result.Notes = append(result.Notes, "Some PR branch references/worktrees were preserved; inspect cleanup actions.")
			break
		}
	}
	return result, nil
}
