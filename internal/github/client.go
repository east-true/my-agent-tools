package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/east-true/my-agent-tools/internal/command"
)

type Client struct {
	Runner command.Runner
	API    API
}

type Repository struct {
	Name          string          `json:"full_name"`
	NodeID        string          `json:"node_id"`
	DefaultBranch string          `json:"default_branch"`
	Permissions   map[string]bool `json:"permissions,omitempty"`
}

type Catalog struct {
	Repository Repository  `json:"repository"`
	Labels     []Label     `json:"labels"`
	IssueTypes []IssueType `json:"issue_types"`
	Notes      []string    `json:"notes,omitempty"`
}

type Plan struct {
	Kind          string         `json:"kind"`
	Repo          string         `json:"repo"`
	Payload       map[string]any `json:"payload"`
	Labels        []string       `json:"-"`
	Selection     Selection      `json:"selection"`
	BranchPlan    *BranchPlan    `json:"branch,omitempty"`
	DefaultBranch string         `json:"-"`
	Template      string         `json:"template,omitempty"`
	Notes         []string       `json:"notes,omitempty"`
	WorkType      string         `json:"-"`
	Slug          string         `json:"-"`
}

type Result struct {
	Status      string         `json:"status"`
	Kind        string         `json:"kind"`
	Repo        string         `json:"repo"`
	Number      int            `json:"number"`
	URL         string         `json:"url"`
	Branch      string         `json:"suggested_branch,omitempty"`
	Notes       []string       `json:"notes,omitempty"`
	Error       string         `json:"error,omitempty"`
	Selection   Selection      `json:"selection"`
	BranchInfo  *BranchOutcome `json:"branch,omitempty"`
	IssueNodeID string         `json:"-"`
}

type resource struct {
	Number    int     `json:"number"`
	NodeID    string  `json:"node_id"`
	Title     string  `json:"title"`
	URL       string  `json:"html_url"`
	Labels    []Label `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	IssueType   *IssueType      `json:"type"`
	PullRequest json.RawMessage `json:"pull_request"`
}

var repoPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*/[a-zA-Z0-9_.-]+$`)

func (client Client) ResolveRepo(ctx context.Context, repo string) (string, error) {
	if repo == "" {
		out, err := client.Runner.Run(ctx, nil, "git", "remote", "get-url", "origin")
		if err != nil {
			return "", fmt.Errorf("resolve repository (or supply --repo OWNER/REPO): %w", err)
		}
		repo, err = repoFromRemote(strings.TrimSpace(string(out)))
		if err != nil {
			return "", err
		}
	}
	if !repoPattern.MatchString(repo) || strings.HasSuffix(repo, "/.") || strings.HasSuffix(repo, "/..") {
		return "", errors.New("repo must be OWNER/REPO")
	}
	return repo, nil
}

func repoFromRemote(remote string) (string, error) {
	if strings.HasPrefix(remote, "git@github.com:") {
		return strings.TrimSuffix(strings.TrimPrefix(remote, "git@github.com:"), ".git"), nil
	}
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Hostname() != "github.com" || (parsed.Scheme != "https" && parsed.Scheme != "ssh") {
		return "", errors.New("origin must be a github.com HTTPS/SSH remote, or supply --repo OWNER/REPO")
	}
	return strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/"), ".git"), nil
}

func (client Client) api(ctx context.Context, method, endpoint string, payload any, target any) error {
	_, err := client.API.Do(ctx, method, endpoint, payload, target)
	return err
}

func (client Client) labels(ctx context.Context, repo string) ([]Label, error) {
	all := []Label{}
	for pageNumber := 1; ; {
		var page []Label
		next, err := client.API.Do(ctx, "GET", fmt.Sprintf("repos/%s/labels?per_page=100&page=%d", repo, pageNumber), nil, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if next == 0 {
			return all, nil
		}
		if next <= pageNumber {
			return nil, errors.New("GitHub returned a non-advancing pagination link")
		}
		pageNumber = next
	}
}

func (client Client) Context(ctx context.Context, repo string) (Catalog, error) {
	var catalog Catalog
	if err := client.api(ctx, "GET", "repos/"+repo, nil, &catalog.Repository); err != nil {
		return catalog, fmt.Errorf("repository metadata: %w", err)
	}
	labels, err := client.labels(ctx, repo)
	if err != nil {
		return catalog, fmt.Errorf("repository labels: %w", err)
	}
	catalog.Labels = labels
	catalog.IssueTypes = []IssueType{}
	if err := client.api(ctx, "GET", "repos/"+repo+"/issue-types", nil, &catalog.IssueTypes); err != nil {
		if !isNotFound(err) {
			return catalog, fmt.Errorf("repository issue types: %w", err)
		}
		catalog.Notes = append(catalog.Notes, "issue types endpoint unavailable (HTTP 404); labels remain available")
	}
	return catalog, nil
}

func (client Client) Prepare(ctx context.Context, repo, kind string, spec Spec, policy Policy) (Plan, error) {
	if kind != "issue" && kind != "pr" {
		return Plan{}, errors.New("kind must be issue or pr")
	}
	if err := spec.ApplyPrefix(); err != nil {
		return Plan{}, err
	}
	body, err := spec.Validate(kind, policy.BodyLanguage)
	if err != nil {
		return Plan{}, err
	}
	catalog, err := client.Context(ctx, repo)
	if err != nil {
		return Plan{}, err
	}
	template, err := client.templates(ctx, repo, kind, spec.Prefix)
	if err != nil {
		return Plan{}, err
	}
	if template != nil {
		body = applyTemplate(*template, body)
	}
	workType, slug, err := branchParts(spec.Title)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Kind: kind, Repo: repo, Notes: catalog.Notes, DefaultBranch: catalog.Repository.DefaultBranch,
		WorkType: workType, Slug: slug, Payload: map[string]any{"title": spec.Title, "body": body}}
	if template != nil {
		plan.Template = template.Filename
	}
	selectMetadata := func() error {
		selection, err := policy.Select(spec, catalog.Labels, catalog.IssueTypes, kind)
		if err != nil {
			return err
		}
		plan.Selection, plan.Labels = selection, selection.Labels
		plan.Notes = append(plan.Notes, selection.Notes...)
		return nil
	}
	if kind == "issue" {
		if err := selectMetadata(); err != nil {
			return Plan{}, err
		}
		var user struct {
			Login string `json:"login"`
		}
		if err := client.api(ctx, "GET", "user", nil, &user); err != nil {
			return Plan{}, fmt.Errorf("resolve @me: %w", err)
		}
		if user.Login == "" {
			return Plan{}, errors.New("GitHub returned an empty authenticated login")
		}
		plan.Payload["assignees"] = []string{user.Login}
		plan.Payload["labels"] = plan.Labels
		issueType := plan.Selection.IssueType
		if issueType != "" {
			if catalog.Repository.Permissions != nil && !catalog.Repository.Permissions["push"] {
				return Plan{}, errors.New("setting an issue type requires repository push permission")
			}
			plan.Payload["type"] = issueType
		}
		return plan, nil
	}
	head := spec.Head
	if head == "" {
		out, err := client.Runner.Run(ctx, nil, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
		if err != nil {
			return Plan{}, fmt.Errorf("resolve current branch (or set head in input): %w", err)
		}
		head = strings.TrimSpace(string(out))
	}
	base := spec.Base
	if base == "" {
		base = catalog.Repository.DefaultBranch
	}
	if base == "" || head == "" {
		return Plan{}, errors.New("PR requires nonempty base and head branches")
	}
	if strings.ContainsAny(base+head, "\r\n\x00") {
		return Plan{}, errors.New("base and head must be branch names")
	}
	branch := head
	if _, after, ok := strings.Cut(head, ":"); ok {
		branch = after
	}
	if spec.Issue == 0 {
		if match := regexp.MustCompile(`^([1-9][0-9]*)-[a-z]+-`).FindStringSubmatch(branch); len(match) > 0 {
			issue, err := strconv.Atoi(match[1])
			if err != nil {
				return Plan{}, errors.New("issue number in branch exceeds supported range")
			}
			spec.Issue = issue
			plan.Payload["body"] = plan.Payload["body"].(string) + fmt.Sprintf("\n\nCloses #%d", issue)
		}
	}
	pattern := workType + `/[a-z0-9]+(?:-[a-z0-9]+)*`
	if spec.Issue > 0 {
		pattern = fmt.Sprintf(`%d-%s-[a-z0-9]+(?:-[a-z0-9]+)*`, spec.Issue, workType)
	}
	if !regexp.MustCompile("^" + pattern + "$").MatchString(branch) {
		return Plan{}, fmt.Errorf("head must follow branch convention %s", pattern)
	}
	if spec.Issue > 0 {
		var linked resource
		if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/issues/%d", repo, spec.Issue), nil, &linked); err != nil {
			return Plan{}, fmt.Errorf("linked issue: %w", err)
		}
		if len(linked.PullRequest) > 0 && string(linked.PullRequest) != "null" {
			return Plan{}, errors.New("issue references a pull request, not an issue")
		}
	}
	var comparison struct {
		AheadBy int `json:"ahead_by"`
	}
	if err := client.api(ctx, "GET", "repos/"+repo+"/compare/"+url.PathEscape(base)+"..."+url.PathEscape(head), nil, &comparison); err != nil {
		return Plan{}, fmt.Errorf("compare remote branches; push the head branch first: %w", err)
	}
	if comparison.AheadBy == 0 {
		return Plan{}, errors.New("remote head has no commits ahead of base; commit and push changes first")
	}
	if err := selectMetadata(); err != nil {
		return Plan{}, err
	}
	plan.Payload["base"], plan.Payload["head"], plan.Payload["draft"] = base, head, spec.Draft
	plan.Payload["maintainer_can_modify"] = true
	return plan, nil
}

// Create does not retry mutations. A failure after creation retains the resource URL.
func (client Client) Create(ctx context.Context, plan Plan) (Result, error) {
	result := Result{Status: "created", Kind: plan.Kind, Repo: plan.Repo, Notes: plan.Notes, Selection: plan.Selection}
	endpoint := "repos/" + plan.Repo + "/issues"
	if plan.Kind == "pr" {
		endpoint = "repos/" + plan.Repo + "/pulls"
	}
	var created resource
	if err := client.api(ctx, "POST", endpoint, plan.Payload, &created); err != nil {
		return Result{}, fmt.Errorf("creation failed or outcome unknown; check GitHub before retrying: %w", err)
	}
	result.Number, result.URL = created.Number, created.URL
	result.IssueNodeID = created.NodeID
	if created.Number <= 0 || created.URL == "" {
		result.Status = "partial"
		result.Notes = append(result.Notes, "creation response lacks number or URL; check GitHub before retrying")
		return result, errors.New("GitHub creation response lacks number or URL; check GitHub before retrying")
	}
	if plan.Kind == "issue" {
		result.Branch = fmt.Sprintf("%d-%s-%s", created.Number, plan.WorkType, plan.Slug)
		for _, login := range plan.Payload["assignees"].([]string) {
			found := false
			for _, assignee := range created.Assignees {
				found = found || strings.EqualFold(login, assignee.Login)
			}
			if !found {
				result.Notes = append(result.Notes, "GitHub did not apply assignee "+login)
				result.Status = "partial"
			}
		}
		if expected, ok := plan.Payload["type"].(string); ok && (created.IssueType == nil || !strings.EqualFold(created.IssueType.Name, expected)) {
			result.Notes = append(result.Notes, "GitHub did not apply issue type "+expected)
			result.Status = "partial"
		}
	} else if len(plan.Labels) > 0 {
		if err := client.api(ctx, "POST", fmt.Sprintf("repos/%s/issues/%d/labels", plan.Repo, created.Number), map[string]any{"labels": plan.Labels}, &created.Labels); err != nil {
			result.Status = "partial"
			result.Notes = append(result.Notes, "PR created; label assignment failed")
			return result, fmt.Errorf("PR %s exists; label assignment: %w", result.URL, err)
		}
	}
	var applied []string
	for _, item := range created.Labels {
		applied = append(applied, item.Name)
	}
	for _, expected := range plan.Labels {
		if match([]string{expected}, applied) == "" {
			result.Status = "partial"
			result.Notes = append(result.Notes, "GitHub did not apply label "+expected)
		}
	}
	if result.Status == "partial" {
		return result, fmt.Errorf("%s %s exists but some metadata was not applied; do not recreate", plan.Kind, result.URL)
	}
	return result, nil
}
