package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type SubmitOptions struct {
	Spec    Spec
	Policy  Policy
	Remote  string
	Wait    bool
	DryRun  bool
	Inspect InspectOptions
}

type SubmitResult struct {
	Status        string         `json:"status"`
	Repo          string         `json:"repo"`
	Number        int            `json:"number,omitempty"`
	URL           string         `json:"url,omitempty"`
	Branch        string         `json:"branch"`
	HeadSHA       string         `json:"head_sha"`
	PushAttempted bool           `json:"push_attempted"`
	Pushed        bool           `json:"pushed"`
	Resume        string         `json:"resume,omitempty"`
	Reused        bool           `json:"reused"`
	Creation      *Result        `json:"creation,omitempty"`
	Inspection    *InspectResult `json:"inspection,omitempty"`
	Error         string         `json:"error,omitempty"`
}

func (client Client) SubmitPR(ctx context.Context, repo string, options SubmitOptions) (SubmitResult, error) {
	result := SubmitResult{Status: "error", Repo: repo}
	fail := func(err error) (SubmitResult, error) {
		result.Error = err.Error()
		if result.PushAttempted {
			result.Status = "partial"
			if !result.Pushed {
				result.Status = "unknown"
			}
			result.Resume = "Inspect the remote branch and open PR; rerun submit to reuse the existing PR after resolving the reported problem."
		}
		return result, err
	}
	if options.Remote == "" {
		options.Remote = "origin"
	}
	if strings.HasPrefix(options.Remote, "-") || strings.ContainsAny(options.Remote, "\r\n\x00") {
		return fail(errors.New("invalid remote name"))
	}
	if err := options.Spec.ApplyPrefix(); err != nil {
		return fail(err)
	}
	if _, err := options.Spec.Validate("pr", options.Policy.BodyLanguage); err != nil {
		return fail(err)
	}
	options.Inspect.Number, options.Inspect.Wait = 1, options.Wait
	if err := options.Inspect.Normalize(); err != nil {
		return fail(err)
	}
	for _, args := range [][]string{{"remote", "get-url", options.Remote}, {"remote", "get-url", "--push", "--all", options.Remote}} {
		out, err := client.Runner.Run(ctx, nil, "git", args...)
		if err != nil {
			return fail(err)
		}
		urls := strings.Fields(string(out))
		if len(urls) != 1 {
			return fail(errors.New("submit requires a single fetch and push URL"))
		}
		actual, err := repoFromRemote(urls[0])
		if err != nil || !strings.EqualFold(actual, repo) {
			return fail(errors.New("submit remote must match the target repository"))
		}
	}
	branch, err := client.Runner.Run(ctx, nil, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return fail(err)
	}
	result.Branch = strings.TrimSpace(string(branch))
	workType, _, err := branchParts(options.Spec.Title)
	if err != nil {
		return fail(err)
	}
	pattern := `^(?:[1-9][0-9]*-` + workType + `-|` + workType + `/)[a-z0-9]+(?:-[a-z0-9]+)*$`
	if !regexp.MustCompile(pattern).MatchString(result.Branch) || (options.Spec.Head != "" && options.Spec.Head != result.Branch) {
		return fail(errors.New("current branch must match the PR prefix and configured head"))
	}
	if options.Spec.Issue > 0 && !strings.HasPrefix(result.Branch, fmt.Sprintf("%d-%s-", options.Spec.Issue, workType)) {
		return fail(errors.New("current branch must match the linked issue number"))
	}
	status, err := client.Runner.Run(ctx, nil, "git", "status", "--porcelain")
	if err != nil || len(strings.TrimSpace(string(status))) != 0 {
		if err == nil {
			err = errors.New("commit or preserve local changes before submitting; submit only pushes existing commits")
		}
		return fail(err)
	}
	sha, err := client.Runner.Run(ctx, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return fail(err)
	}
	result.HeadSHA = strings.TrimSpace(string(sha))
	if !commitSHA.MatchString(result.HeadSHA) {
		return fail(errors.New("current branch has no valid commit SHA"))
	}
	if options.DryRun {
		result.Status = "planned"
		return result, nil
	}
	options.Spec.Head = result.Branch
	before, err := client.submitPRs(ctx, repo, result.Branch)
	if err != nil {
		return fail(err)
	}
	if len(before) > 1 {
		return fail(errors.New("multiple open PRs use this branch; select a PR explicitly"))
	}
	if len(before) == 1 {
		if err := validateSubmitPR(before[0], repo, result.Branch, options.Spec.Base, ""); err != nil {
			return fail(err)
		}
		if options.Spec.Base == "" {
			options.Spec.Base = before[0].Base.Ref
		}
		result.Number, result.URL = before[0].Number, before[0].URL
	}
	plan, err := client.prepare(ctx, repo, "pr", options.Spec, options.Policy, false)
	if err != nil {
		return fail(err)
	}
	var baseRef struct {
		Ref string `json:"ref"`
	}
	if err := client.api(ctx, "GET", "repos/"+repo+"/git/ref/heads/"+url.PathEscape(plan.Payload["base"].(string)), nil, &baseRef); err != nil {
		return fail(fmt.Errorf("validate PR base before push: %w", err))
	}
	if baseRef.Ref != "refs/heads/"+plan.Payload["base"].(string) {
		return fail(errors.New("PR base branch metadata is incomplete"))
	}
	result.PushAttempted = true
	if _, err := client.Runner.Run(ctx, nil, "git", "push", "--porcelain", "--no-follow-tags", "--", options.Remote, result.HeadSHA+":refs/heads/"+result.Branch); err != nil {
		return fail(fmt.Errorf("push failed or outcome unknown; check the remote before retrying: %w", err))
	}
	result.Pushed = true
	var ref struct {
		Object struct{ SHA string } `json:"object"`
	}
	if err := client.api(ctx, "GET", "repos/"+repo+"/git/ref/heads/"+url.PathEscape(result.Branch), nil, &ref); err != nil {
		return fail(err)
	}
	if ref.Object.SHA != result.HeadSHA {
		return fail(errors.New("remote branch changed or does not contain the pushed commit; inspect before continuing"))
	}
	existing, err := client.submitPRs(ctx, repo, result.Branch)
	if err != nil {
		return fail(err)
	}
	if len(existing) > 1 {
		return fail(errors.New("multiple open PRs use this branch; select a PR explicitly"))
	}
	if len(existing) == 1 {
		item := existing[0]
		if err := validateSubmitPR(item, repo, result.Branch, plan.Payload["base"].(string), result.HeadSHA); err != nil {
			return fail(err)
		}
		result.Number, result.URL, result.Reused = item.Number, item.URL, true
	} else {
		if len(before) != 0 {
			return fail(errors.New("the selected PR closed or disappeared after push; inspect it before continuing"))
		}
		if err := client.comparePRBranches(ctx, repo, plan.Payload["base"].(string), result.Branch); err != nil {
			return fail(err)
		}
		creation, err := client.Create(ctx, plan)
		result.Creation = &creation
		result.Number, result.URL = creation.Number, creation.URL
		if err != nil {
			return fail(err)
		}
	}
	result.Status = "submitted"
	{
		options.Inspect.Number = result.Number
		inspection, err := client.InspectPR(ctx, repo, options.Inspect)
		result.Inspection = &inspection
		if err != nil {
			return fail(err)
		}
		if inspection.PR.HeadSHA != result.HeadSHA || !inspection.Complete {
			result.Status = "partial"
			if inspection.Status == "timeout" || inspection.Status == "cancelled" {
				result.Status = inspection.Status
			}
		} else {
			for _, reason := range inspection.Reasons {
				if reason.Code == "check_failed" || reason.Code == "status_failed" {
					result.Status = "failed"
				}
			}
		}
	}
	return result, nil
}

type submitPR struct {
	cleanupPR
	URL  string `json:"html_url"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (client Client) submitPRs(ctx context.Context, repo, branch string) ([]submitPR, error) {
	owner, _, _ := strings.Cut(repo, "/")
	return cleanupPages[submitPR](ctx, client, "repos/"+repo+"/pulls?state=open&head="+url.QueryEscape(owner+":"+branch))
}

func validateSubmitPR(pr submitPR, repo, branch, base, sha string) error {
	if pr.Number <= 0 || pr.URL == "" || pr.State != "open" || pr.Head.Ref != branch || pr.Head.Repo == nil || !strings.EqualFold(pr.Head.Repo.Name, repo) || pr.Base.Ref == "" || pr.Head.SHA == "" {
		return errors.New("existing PR source/base metadata does not match the requested repository and branch")
	}
	if base != "" && pr.Base.Ref != base {
		return errors.New("existing PR base differs from the requested base; select or update the PR explicitly")
	}
	if sha != "" && pr.Head.SHA != sha {
		return errors.New("existing PR has not observed the pushed commit; inspect the PR and retry without creating another")
	}
	return nil
}
