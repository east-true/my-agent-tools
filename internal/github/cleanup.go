package github

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type CleanupOptions struct {
	Remote  string   `json:"remote"`
	Scope   string   `json:"scope"`
	Protect []string `json:"protect,omitempty"`
}

type CleanupTarget struct {
	Scope        string   `json:"scope"`
	Name         string   `json:"name"`
	RemoteBranch string   `json:"remote_branch"`
	SHA          string   `json:"sha"`
	Eligible     bool     `json:"eligible"`
	Reasons      []string `json:"reasons,omitempty"`
	Skip         string   `json:"skip,omitempty"`
}

type CleanupPlan struct {
	Repo          string          `json:"repo"`
	DefaultBranch string          `json:"default_branch"`
	Options       CleanupOptions  `json:"options"`
	Targets       []CleanupTarget `json:"targets"`
}

type CleanupAction struct {
	CleanupTarget
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
}

type CleanupResult struct {
	Status  string          `json:"status"`
	Repo    string          `json:"repo"`
	Actions []CleanupAction `json:"actions"`
}

type cleanupRemoteBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Commit    struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

type cleanupPR struct {
	Number   int     `json:"number"`
	State    string  `json:"state"`
	MergedAt *string `json:"merged_at"`
	Head     struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			Name string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

type cleanupLocalBranch struct{ name, sha, remote, remoteRef string }

func cleanupPages[T any](ctx context.Context, client Client, endpoint string) ([]T, error) {
	items := []T{}
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	for page := 1; ; {
		var batch []T
		next, err := client.API.Do(ctx, "GET", fmt.Sprintf("%s%sper_page=100&page=%d", endpoint, separator, page), nil, &batch)
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if next == 0 {
			return items, nil
		}
		if next <= page {
			return nil, errors.New("cleanup pagination did not advance")
		}
		page = next
	}
}

type cleanupPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type cleanupLinkedBranches struct {
	Nodes []struct {
		Ref *struct {
			Name       string `json:"name"`
			Repository struct {
				Name string `json:"nameWithOwner"`
			} `json:"repository"`
		} `json:"ref"`
	} `json:"nodes"`
	PageInfo cleanupPageInfo `json:"pageInfo"`
}

// Closed issues are queried with their Development links, including nested pagination.
func (client Client) cleanupClosedIssues(ctx context.Context, repo string) (map[string][]int, map[int]bool, error) {
	const query = `query CleanupIssues($owner:String!,$name:String!,$cursor:String){repository(owner:$owner,name:$name){issues(states:CLOSED,first:100,after:$cursor){nodes{id number linkedBranches(first:100){nodes{ref{name repository{nameWithOwner}}}pageInfo{hasNextPage endCursor}}}pageInfo{hasNextPage endCursor}}}}`
	const more = `query CleanupIssueBranches($id:ID!,$cursor:String!){node(id:$id){... on Issue{linkedBranches(first:100,after:$cursor){nodes{ref{name repository{nameWithOwner}}}pageInfo{hasNextPage endCursor}}}}}`
	owner, name, _ := strings.Cut(repo, "/")
	links, closed := map[string][]int{}, map[int]bool{}
	var cursor any
	seen := map[string]bool{}
	for {
		var response struct {
			graphErrors
			Data struct {
				Repository *struct {
					Issues struct {
						Nodes []struct {
							ID     string                `json:"id"`
							Number int                   `json:"number"`
							Linked cleanupLinkedBranches `json:"linkedBranches"`
						} `json:"nodes"`
						PageInfo cleanupPageInfo `json:"pageInfo"`
					} `json:"issues"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := client.api(ctx, "POST", "graphql", map[string]any{"query": query, "variables": map[string]any{"owner": owner, "name": name, "cursor": cursor}}, &response); err != nil {
			return nil, nil, err
		}
		if err := response.err(); err != nil {
			return nil, nil, err
		}
		if response.Data.Repository == nil {
			return nil, nil, errors.New("GitHub returned no repository when reading closed issues")
		}
		connection := response.Data.Repository.Issues
		for _, issue := range connection.Nodes {
			if issue.Number <= 0 {
				return nil, nil, errors.New("closed issue lacks its number")
			}
			closed[issue.Number] = true
			linked, nestedSeen := issue.Linked, map[string]bool{}
			for {
				for _, node := range linked.Nodes {
					if node.Ref != nil && strings.EqualFold(node.Ref.Repository.Name, repo) {
						links[node.Ref.Name] = append(links[node.Ref.Name], issue.Number)
					}
				}
				if !linked.PageInfo.HasNextPage {
					break
				}
				next := linked.PageInfo.EndCursor
				if next == "" || nestedSeen[next] || issue.ID == "" {
					return nil, nil, errors.New("linked branch pagination did not advance")
				}
				nestedSeen[next] = true
				var nested struct {
					graphErrors
					Data struct {
						Node *struct {
							Linked cleanupLinkedBranches `json:"linkedBranches"`
						} `json:"node"`
					} `json:"data"`
				}
				if err := client.api(ctx, "POST", "graphql", map[string]any{"query": more, "variables": map[string]any{"id": issue.ID, "cursor": next}}, &nested); err != nil {
					return nil, nil, err
				}
				if err := nested.err(); err != nil {
					return nil, nil, err
				}
				if nested.Data.Node == nil {
					return nil, nil, errors.New("linked issue disappeared during cleanup planning")
				}
				linked = nested.Data.Node.Linked
			}
		}
		if !connection.PageInfo.HasNextPage {
			return links, closed, nil
		}
		next := connection.PageInfo.EndCursor
		if next == "" || seen[next] {
			return nil, nil, errors.New("closed issue pagination did not advance")
		}
		seen[next], cursor = true, next
	}
}

func (client Client) cleanupGit(ctx context.Context, options CleanupOptions, repo string) ([]cleanupLocalBranch, map[string]bool, error) {
	if options.Remote == "" || strings.HasPrefix(options.Remote, "-") || strings.ContainsAny(options.Remote, "\r\n\x00") {
		return nil, nil, errors.New("invalid remote name")
	}
	// A distinct push URL must also identify the selected repository.
	for _, args := range [][]string{{"remote", "get-url", options.Remote}, {"remote", "get-url", "--push", "--all", options.Remote}} {
		out, err := client.Runner.Run(ctx, nil, "git", args...)
		if err != nil {
			return nil, nil, err
		}
		values := strings.Fields(string(out))
		if len(values) != 1 {
			return nil, nil, errors.New("cleanup requires a single remote fetch/push URL")
		}
		actual, err := repoFromRemote(values[0])
		if err != nil || !strings.EqualFold(actual, repo) {
			return nil, nil, errors.New("cleanup remote must match the target GitHub repository for fetch and push")
		}
	}
	out, err := client.Runner.Run(ctx, nil, "git", "for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/")
	if err != nil {
		return nil, nil, err
	}
	local := []cleanupLocalBranch{}
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 4 || !strings.HasPrefix(fields[0], "refs/heads/") || fields[1] == "" {
			return nil, nil, errors.New("invalid local branch inventory")
		}
		local = append(local, cleanupLocalBranch{strings.TrimPrefix(fields[0], "refs/heads/"), fields[1], fields[2], strings.TrimPrefix(fields[3], "refs/heads/")})
	}
	checked, err := client.cleanupWorktrees(ctx, options.Remote)
	for _, branch := range local {
		if checked[branch.name] && branch.remote == options.Remote && branch.remoteRef != "" {
			checked[branch.remoteRef] = true
		}
	}
	return local, checked, err
}

func (client Client) cleanupWorktrees(ctx context.Context, remote string) (map[string]bool, error) {
	out, err := client.Runner.Run(ctx, nil, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	checked := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if branch, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
			checked[branch] = true
		}
	}
	// A checked-out local alias also protects the remote branch it tracks.
	out, err = client.Runner.Run(ctx, nil, "git", "for-each-ref", "--format=%(refname)%00%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 3 {
			return nil, errors.New("invalid worktree branch inventory")
		}
		if checked[strings.TrimPrefix(fields[0], "refs/heads/")] && fields[1] == remote {
			checked[strings.TrimPrefix(fields[2], "refs/heads/")] = true
		}
	}
	return checked, nil
}

var cleanupIssueNumber = regexp.MustCompile(`^([1-9][0-9]*)-[a-z]+-`)

// PlanCleanup is read-only: it neither fetches nor changes Git refs or config.
func (client Client) PlanCleanup(ctx context.Context, repo string, options CleanupOptions) (CleanupPlan, error) {
	plan := CleanupPlan{Repo: repo, Options: options, Targets: []CleanupTarget{}}
	if options.Scope != "both" && options.Scope != "local" && options.Scope != "remote" {
		return plan, errors.New("scope must be both, local, or remote")
	}
	for _, pattern := range options.Protect {
		if _, err := path.Match(pattern, ""); err != nil {
			return plan, fmt.Errorf("invalid protection pattern: %w", err)
		}
	}
	local, checked, err := client.cleanupGit(ctx, options, repo)
	if err != nil {
		return plan, err
	}
	var repository Repository
	if err := client.api(ctx, "GET", "repos/"+repo, nil, &repository); err != nil {
		return plan, err
	}
	if repository.DefaultBranch == "" {
		return plan, errors.New("repository has no default branch")
	}
	plan.DefaultBranch = repository.DefaultBranch
	branches, err := cleanupPages[cleanupRemoteBranch](ctx, client, "repos/"+repo+"/branches")
	if err != nil {
		return plan, err
	}
	remote := map[string]cleanupRemoteBranch{}
	for _, branch := range branches {
		if branch.Name == "" || branch.Commit.SHA == "" {
			return plan, errors.New("remote branch lacks name or commit")
		}
		remote[branch.Name] = branch
	}
	prs, err := cleanupPages[cleanupPR](ctx, client, "repos/"+repo+"/pulls?state=all&sort=created&direction=desc")
	if err != nil {
		return plan, err
	}
	open, latest := map[string]bool{}, map[string]cleanupPR{}
	for _, pr := range prs {
		if pr.Head.Repo == nil || !strings.EqualFold(pr.Head.Repo.Name, repo) {
			continue
		}
		if pr.State == "open" {
			open[pr.Head.Ref] = true
		}
		if pr.Number > latest[pr.Head.Ref].Number {
			latest[pr.Head.Ref] = pr
		}
	}
	links, closed, err := client.cleanupClosedIssues(ctx, repo)
	if err != nil {
		return plan, err
	}
	issueChecked := map[int]bool{}
	protected := func(name string) bool {
		if name == repository.DefaultBranch || name == "main" || name == "master" || checked[name] || remote[name].Protected {
			return true
		}
		for _, pattern := range options.Protect {
			if matched, _ := path.Match(pattern, name); matched {
				return true
			}
		}
		return false
	}
	reasons := func(name string) ([]string, string, error) {
		if protected(name) {
			return nil, "protected or checked out in a worktree", nil
		}
		if open[name] {
			return nil, "open PR", nil
		}
		reason := []string{}
		pr, hasPR := latest[name]
		if hasPR && pr.State == "closed" {
			if sha := remote[name].Commit.SHA; sha != "" && sha != pr.Head.SHA {
				return nil, "branch advanced since its latest closed PR", nil
			}
			if pr.MergedAt != nil {
				reason = append(reason, fmt.Sprintf("PR #%d merged", pr.Number))
			} else {
				reason = append(reason, fmt.Sprintf("PR #%d closed", pr.Number))
			}
		}
		for _, number := range links[name] {
			reason = append(reason, fmt.Sprintf("issue #%d closed (Development link)", number))
		}
		if match := cleanupIssueNumber.FindStringSubmatch(name); match != nil {
			number, err := strconv.Atoi(match[1])
			if err != nil {
				return nil, "invalid issue number", nil
			}
			if !closed[number] && !issueChecked[number] {
				var issue struct {
					State       string `json:"state"`
					PullRequest any    `json:"pull_request"`
				}
				err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/issues/%d", repo, number), nil, &issue)
				if err != nil && !isNotFound(err) {
					return nil, "", err
				}
				issueChecked[number] = true
				closed[number] = err == nil && issue.State == "closed" && issue.PullRequest == nil
			}
			if closed[number] {
				reason = append(reason, fmt.Sprintf("issue #%d closed (branch number)", number))
			}
		}
		if len(reason) == 0 {
			return nil, "no merged/closed PR or closed issue", nil
		}
		sort.Strings(reason)
		return reason, "", nil
	}
	if options.Scope != "local" {
		names := make([]string, 0, len(remote))
		for name := range remote {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			reason, skip, err := reasons(name)
			if err != nil {
				return plan, err
			}
			plan.Targets = append(plan.Targets, CleanupTarget{Scope: "remote", Name: name, RemoteBranch: name, SHA: remote[name].Commit.SHA, Eligible: skip == "", Reasons: reason, Skip: skip})
		}
	}
	if options.Scope != "remote" {
		out, err := client.Runner.Run(ctx, nil, "git", "for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)", "refs/remotes/"+options.Remote+"/")
		if err != nil {
			return plan, err
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
			if line == "" {
				continue
			}
			fields := strings.Split(line, "\x00")
			prefix := "refs/remotes/" + options.Remote + "/"
			if len(fields) != 3 || !strings.HasPrefix(fields[0], prefix) || fields[1] == "" {
				return plan, errors.New("invalid remote-tracking branch inventory")
			}
			name := strings.TrimPrefix(fields[0], prefix)
			if fields[2] != "" || remote[name].Name != "" {
				continue
			}
			reason, skip, err := reasons(name)
			if err != nil {
				return plan, err
			}
			plan.Targets = append(plan.Targets, CleanupTarget{Scope: "tracking", Name: options.Remote + "/" + name, RemoteBranch: name, SHA: fields[1], Eligible: skip == "", Reasons: reason, Skip: skip})
		}
	}
	for _, branch := range local {
		if options.Scope == "remote" {
			break
		}
		name := branch.name
		if branch.remote == options.Remote && branch.remoteRef != "" {
			name = branch.remoteRef
		}
		target := CleanupTarget{Scope: "local", Name: branch.name, RemoteBranch: name, SHA: branch.sha}
		if branch.remote != "" && branch.remote != options.Remote {
			target.Skip = "upstream belongs to another remote"
		} else if protected(branch.name) {
			target.Skip = "protected or checked out in a worktree"
		} else {
			target.Reasons, target.Skip, err = reasons(name)
			if err != nil {
				return plan, err
			}
			if target.Skip == "" {
				// Never discard unpublished local work, even after a squash merge.
				anchors := []string{remote[name].Commit.SHA, latest[name].Head.SHA, "refs/remotes/" + options.Remote + "/" + repository.DefaultBranch}
				if branch.remote == options.Remote && branch.remoteRef != "" {
					anchors = append(anchors, "refs/remotes/"+options.Remote+"/"+branch.remoteRef)
				}
				safe := false
				for _, anchor := range anchors {
					if anchor != "" {
						if _, err := client.Runner.Run(ctx, nil, "git", "merge-base", "--is-ancestor", branch.sha, anchor); err == nil {
							safe = true
							break
						}
					}
				}
				if !safe {
					target.Skip = "local commits are unpublished or publication cannot be verified"
				}
			}
		}
		target.Eligible = target.Skip == ""
		plan.Targets = append(plan.Targets, target)
	}
	return plan, nil
}

// ApplyCleanup refreshes terminal states/protections and uses expected SHAs for deletion.
func (client Client) ApplyCleanup(ctx context.Context, plan CleanupPlan) (CleanupResult, error) {
	result := CleanupResult{Status: "completed", Repo: plan.Repo, Actions: []CleanupAction{}}
	fresh, err := client.PlanCleanup(ctx, plan.Repo, plan.Options)
	if err != nil {
		return result, err
	}
	current := map[string]CleanupTarget{}
	for _, target := range fresh.Targets {
		current[target.Scope+"\x00"+target.Name] = target
	}
	remoteFailed := map[string]bool{}
	var failures []error
	for _, target := range plan.Targets {
		action := CleanupAction{CleanupTarget: target, Status: "skipped"}
		if !target.Eligible {
			result.Actions = append(result.Actions, action)
			continue
		}
		now, exists := current[target.Scope+"\x00"+target.Name]
		if !exists || !now.Eligible || now.SHA != target.SHA || now.RemoteBranch != target.RemoteBranch || !slices.Equal(now.Reasons, target.Reasons) || fresh.DefaultBranch != plan.DefaultBranch {
			action.Skip = "branch or associated GitHub state changed; preview again"
			if target.Scope == "remote" {
				remoteFailed[target.RemoteBranch] = true
			}
			result.Actions = append(result.Actions, action)
			continue
		}
		if target.Scope != "remote" && remoteFailed[target.RemoteBranch] {
			action.Skip = "remote deletion did not succeed; local branch retained"
			result.Actions = append(result.Actions, action)
			continue
		}
		// Recheck worktrees immediately before every ref deletion.
		checked, err := client.cleanupWorktrees(ctx, plan.Options.Remote)
		if err == nil && (checked[target.Name] || checked[target.RemoteBranch]) {
			action.Skip = "branch is now checked out in a worktree"
			result.Actions = append(result.Actions, action)
			continue
		}
		if err == nil {
			if target.Scope == "remote" {
				ref := "refs/heads/" + target.Name
				_, err = client.Runner.Run(ctx, nil, "git", "-c", "remote."+plan.Options.Remote+".mirror=false", "push", "--porcelain", "--no-follow-tags", "--force-with-lease="+ref+":"+target.SHA, "--", plan.Options.Remote, ":"+ref)
			} else {
				ref := "refs/heads/" + target.Name
				if target.Scope == "tracking" {
					ref = "refs/remotes/" + target.Name
				}
				_, err = client.Runner.Run(ctx, nil, "git", "update-ref", "--no-deref", "-d", ref, target.SHA)
			}
		}
		if err != nil {
			action.Status, action.Error = "error", err.Error()
			failures = append(failures, err)
			if target.Scope == "remote" {
				remoteFailed[target.RemoteBranch] = true
			}
		} else {
			action.Status = "deleted"
			if target.Scope == "local" {
				// update-ref provides the SHA guard; remove the branch's leftover
				// upstream settings separately only when such settings exist.
				pattern := "^branch\\." + regexp.QuoteMeta(target.Name) + "\\."
				if _, existsErr := client.Runner.Run(ctx, nil, "git", "config", "--local", "--get-regexp", pattern); existsErr == nil {
					if _, configErr := client.Runner.Run(ctx, nil, "git", "config", "--local", "--remove-section", "branch."+target.Name); configErr != nil {
						action.Warning = "branch deleted; configuration cleanup failed: " + configErr.Error()
					}
				}
			}
		}
		result.Actions = append(result.Actions, action)
	}
	if len(failures) > 0 {
		result.Status = "partial"
		return result, errors.Join(failures...)
	}
	return result, nil
}
