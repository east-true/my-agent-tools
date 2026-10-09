package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

type BranchPlan struct {
	Name         string `json:"name"`
	Base         string `json:"base"`
	OID          string `json:"base_sha"`
	Checkout     bool   `json:"checkout"`
	WorktreePath string `json:"worktree_path,omitempty"`
}

type BranchOutcome struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Linked         bool   `json:"linked"`
	CheckedOut     bool   `json:"checked_out"`
	Reused         bool   `json:"reused,omitempty"`
	WorktreePath   string `json:"worktree_path,omitempty"`
	WorktreeReused bool   `json:"worktree_reused,omitempty"`
}

func branchParts(title string) (string, string, error) {
	if !titlePattern.MatchString(title) {
		return "", "", errors.New("issue title must follow '<type>: <summary>' to derive a branch name")
	}
	kind, summary, _ := strings.Cut(title, ": ")
	slug := strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(summary), "-"), "-")
	if slug == "" {
		return "", "", errors.New("issue title has no usable branch slug")
	}
	if len(slug) > 180 {
		slug = strings.TrimRight(slug[:180], "-")
	}
	return kind, slug, nil
}

// PrepareBranch reads the remote base without moving local HEAD or fetching.
func (client Client) PrepareBranch(ctx context.Context, plan *Plan, checkout bool) error {
	base := plan.DefaultBranch
	if base == "" {
		return errors.New("repository has no default branch; cannot create a linked branch")
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := client.api(ctx, "GET", "repos/"+plan.Repo+"/git/ref/heads/"+url.PathEscape(base), nil, &ref); err != nil {
		return fmt.Errorf("resolve branch base: %w", err)
	}
	if ref.Object.SHA == "" {
		return errors.New("GitHub returned an empty base commit SHA")
	}
	plan.BranchPlan = &BranchPlan{Name: "<issue-number>-" + plan.WorkType + "-" + plan.Slug, Base: base, OID: ref.Object.SHA}
	if !checkout {
		return nil
	}
	inside, err := client.Runner.Run(ctx, nil, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(string(inside)) != "true" {
		plan.Notes = append(plan.Notes, "local worktree creation skipped: no Git worktree; linked remote branch will be created")
		return nil
	}
	remote, err := client.Runner.Run(ctx, nil, "git", "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("inspect local origin before issue creation: %w", err)
	}
	localRepo, err := repoFromRemote(strings.TrimSpace(string(remote)))
	if err != nil || !strings.EqualFold(localRepo, plan.Repo) {
		plan.Notes = append(plan.Notes, "local worktree creation skipped: origin differs from target repository")
		return nil
	}
	plan.BranchPlan.Checkout = true
	worktrees, err := client.branchWorktrees(ctx)
	if err != nil {
		return fmt.Errorf("inspect local worktrees before issue creation: %w", err)
	}
	root := worktrees[0].Path
	plan.BranchPlan.WorktreePath = filepath.Join(root+".worktrees", plan.BranchPlan.Name)
	return nil
}

// ExistingIssuePlan supports recovery without creating a second issue.
func (client Client) ExistingIssuePlan(ctx context.Context, repo string, number int) (Plan, Result, error) {
	var repository Repository
	if err := client.api(ctx, "GET", "repos/"+repo, nil, &repository); err != nil {
		return Plan{}, Result{}, err
	}
	var issue resource
	if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/issues/%d", repo, number), nil, &issue); err != nil {
		return Plan{}, Result{}, err
	}
	if len(issue.PullRequest) > 0 && string(issue.PullRequest) != "null" {
		return Plan{}, Result{}, errors.New("number refers to a PR, not an issue")
	}
	kind, slug, err := branchParts(issue.Title)
	if err != nil {
		return Plan{}, Result{}, err
	}
	if issue.Number != number || issue.URL == "" || issue.NodeID == "" {
		return Plan{}, Result{}, errors.New("GitHub issue response lacks number, URL or node ID")
	}
	plan := Plan{Kind: "issue", Repo: repo, DefaultBranch: repository.DefaultBranch, WorkType: kind, Slug: slug}
	result := Result{Status: "created", Kind: "branch", Repo: repo, Number: number, URL: issue.URL, IssueNodeID: issue.NodeID, Branch: fmt.Sprintf("%d-%s-%s", number, kind, slug)}
	return plan, result, nil
}

type graphErrors struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (response graphErrors) err() error {
	if len(response.Errors) == 0 {
		return nil
	}
	var messages []string
	for _, item := range response.Errors {
		messages = append(messages, item.Message)
	}
	return fmt.Errorf("GitHub GraphQL: %s", strings.Join(messages, "; "))
}

func (client Client) isLinked(ctx context.Context, issueID, repo, branch string) (bool, error) {
	const query = `query($id:ID!,$cursor:String){node(id:$id){... on Issue{linkedBranches(first:100,after:$cursor){nodes{ref{name repository{nameWithOwner}}}pageInfo{hasNextPage endCursor}}}}}`
	var cursor any
	seen := map[string]bool{}
	for {
		var response struct {
			graphErrors
			Data struct {
				Node *struct {
					LinkedBranches struct {
						Nodes []struct {
							Ref *struct {
								Name       string `json:"name"`
								Repository struct {
									Name string `json:"nameWithOwner"`
								} `json:"repository"`
							} `json:"ref"`
						} `json:"nodes"`
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"linkedBranches"`
				} `json:"node"`
			} `json:"data"`
		}
		if err := client.api(ctx, "POST", "graphql", map[string]any{"query": query, "variables": map[string]any{"id": issueID, "cursor": cursor}}, &response); err != nil {
			return false, err
		}
		if err := response.err(); err != nil {
			return false, err
		}
		if response.Data.Node == nil {
			return false, errors.New("GitHub returned no issue when checking linked branches")
		}
		connection := response.Data.Node.LinkedBranches
		for _, node := range connection.Nodes {
			if node.Ref != nil && node.Ref.Name == branch && strings.EqualFold(node.Ref.Repository.Name, repo) {
				return true, nil
			}
		}
		if !connection.PageInfo.HasNextPage {
			return false, nil
		}
		next := connection.PageInfo.EndCursor
		if next == "" || seen[next] {
			return false, errors.New("linked branch pagination did not advance")
		}
		seen[next] = true
		cursor = next
	}
}

// CompleteBranch keeps the already-created issue and branch in Result on every failure.
func (client Client) CompleteBranch(ctx context.Context, plan Plan, result *Result) error {
	if plan.BranchPlan == nil {
		return nil
	}
	branch := fmt.Sprintf("%d-%s-%s", result.Number, plan.WorkType, plan.Slug)
	result.Branch = branch
	if result.IssueNodeID == "" {
		return errors.New("issue exists but lacks a node ID; cannot link a branch")
	}
	var existing struct {
		Ref string `json:"ref"`
	}
	err := client.api(ctx, "GET", "repos/"+plan.Repo+"/git/ref/heads/"+url.PathEscape(branch), nil, &existing)
	if err == nil {
		linked, err := client.isLinked(ctx, result.IssueNodeID, plan.Repo, branch)
		if err != nil {
			return err
		}
		if !linked {
			return fmt.Errorf("remote branch %s already exists and is not linked to this issue; it was not changed", branch)
		}
		result.BranchInfo = &BranchOutcome{Name: branch, URL: "https://github.com/" + plan.Repo + "/tree/" + branch, Linked: true, Reused: true}
	} else if !isNotFound(err) {
		return fmt.Errorf("inspect remote issue branch: %w", err)
	} else {
		const mutation = `mutation($input:CreateLinkedBranchInput!){createLinkedBranch(input:$input){linkedBranch{ref{name}}}}`
		var response struct {
			graphErrors
			Data struct {
				CreateLinkedBranch struct {
					LinkedBranch *struct {
						Ref *struct {
							Name string `json:"name"`
						} `json:"ref"`
					} `json:"linkedBranch"`
				} `json:"createLinkedBranch"`
			} `json:"data"`
		}
		payload := map[string]any{"query": mutation, "variables": map[string]any{"input": map[string]any{"issueId": result.IssueNodeID, "name": branch, "oid": plan.BranchPlan.OID}}}
		if err := client.api(ctx, "POST", "graphql", payload, &response); err != nil {
			return fmt.Errorf("linked branch creation failed or outcome unknown; inspect GitHub before retrying: %w", err)
		}
		linked := response.Data.CreateLinkedBranch.LinkedBranch
		if linked != nil && linked.Ref != nil && linked.Ref.Name == branch {
			result.BranchInfo = &BranchOutcome{Name: branch, URL: "https://github.com/" + plan.Repo + "/tree/" + branch, Linked: true}
		}
		if err := response.err(); err != nil {
			return err
		}
		if result.BranchInfo == nil {
			return errors.New("GitHub did not confirm the linked branch; inspect GitHub before retrying")
		}
	}
	if plan.BranchPlan.Checkout {
		path := filepath.Join(filepath.Dir(plan.BranchPlan.WorktreePath), branch)
		result.BranchInfo.WorktreePath = path
		actual, reused, err := client.worktreeBranch(ctx, branch, path)
		if err != nil {
			return fmt.Errorf("issue and linked remote branch exist; local worktree failed: %w", err)
		}
		result.BranchInfo.WorktreePath = actual
		result.BranchInfo.WorktreeReused = reused
		result.BranchInfo.CheckedOut = true
	}
	return nil
}
