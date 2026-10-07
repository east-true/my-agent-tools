package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	sdk "github.com/google/go-github/v92/github"
)

type MergeOptions struct {
	Number   int
	Timeout  time.Duration
	Interval time.Duration
	Method   string
}

func (options *MergeOptions) Normalize() error {
	if options.Number <= 0 {
		return errors.New("--number must be a positive PR number")
	}
	if options.Timeout == 0 {
		options.Timeout = 10 * time.Minute
	}
	if options.Interval == 0 {
		options.Interval = 10 * time.Second
	}
	if options.Method == "" {
		options.Method = "squash"
	}
	if options.Timeout < 0 || options.Interval < 0 {
		return errors.New("timeout and interval must be positive")
	}
	if options.Method != "merge" && options.Method != "squash" && options.Method != "rebase" {
		return errors.New("--method must be merge, squash, or rebase")
	}
	return nil
}

type MergeReason struct {
	Code            string `json:"code"`
	Name            string `json:"name,omitempty"`
	Summary         string `json:"summary"`
	URL             string `json:"url,omitempty"`
	NextAction      string `json:"next_action"`
	Details         string `json:"details,omitempty"`
	DiagnosticError string `json:"diagnostic_error,omitempty"`
	checkID         int64
	jobID           int64
}

type MergeResult struct {
	Status         string        `json:"status"`
	Repo           string        `json:"repo"`
	Number         int           `json:"number"`
	URL            string        `json:"url,omitempty"`
	HeadSHA        string        `json:"head_sha,omitempty"`
	MergeSHA       string        `json:"merge_sha,omitempty"`
	RequestID      string        `json:"request_id,omitempty"`
	MergeRequested bool          `json:"merge_requested,omitempty"`
	Queued         bool          `json:"queued,omitempty"`
	Reasons        []MergeReason `json:"reasons,omitempty"`
}

type mergePR struct {
	URL            string `json:"url"`
	State          string `json:"state"`
	IsDraft        bool   `json:"isDraft"`
	Merged         bool   `json:"merged"`
	HeadSHA        string `json:"headRefOid"`
	BaseSHA        string `json:"baseRefOid"`
	Mergeable      string `json:"mergeable"`
	MergeState     string `json:"mergeStateStatus"`
	ReviewDecision string `json:"reviewDecision"`
	MergeCommit    *struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
	TestCommit *struct {
		OID string `json:"oid"`
	} `json:"potentialMergeCommit"`
	QueueEntry *struct {
		ID string `json:"id"`
	} `json:"mergeQueueEntry"`
}

func (client Client) mergePR(ctx context.Context, repo string, number int) (*mergePR, error) {
	owner, name, _ := strings.Cut(repo, "/")
	const query = `query MergePullRequest($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){url state isDraft merged headRefOid baseRefOid mergeable mergeStateStatus reviewDecision mergeCommit{oid} potentialMergeCommit{oid} mergeQueueEntry{id}}}}`
	var response struct {
		graphErrors
		Data struct {
			Repository *struct {
				PR *mergePR `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := client.api(ctx, "POST", "graphql", map[string]any{"query": query, "variables": map[string]any{"owner": owner, "name": name, "number": number}}, &response); err != nil {
		return nil, fmt.Errorf("read PR merge conditions: %w", err)
	}
	if err := response.err(); err != nil {
		return nil, err
	}
	if response.Data.Repository == nil || response.Data.Repository.PR == nil {
		return nil, errors.New("GitHub returned no pull request")
	}
	pr := response.Data.Repository.PR
	if pr.HeadSHA == "" || pr.State == "" || pr.Mergeable == "" || pr.MergeState == "" {
		return nil, errors.New("GitHub returned incomplete PR merge conditions")
	}
	return pr, nil
}

func reason(code, summary, action string) MergeReason {
	return MergeReason{Code: code, Summary: summary, NextAction: action}
}

func (pr *mergePR) blockers() []MergeReason {
	var reasons []MergeReason
	if pr.State != "OPEN" {
		reasons = append(reasons, reason("pr_closed", "Pull request is closed", "Reopen the PR before merging"))
	}
	if pr.IsDraft {
		reasons = append(reasons, reason("draft", "Pull request is a draft", "Mark the PR ready for review"))
	}
	if pr.Mergeable == "CONFLICTING" || pr.MergeState == "DIRTY" {
		reasons = append(reasons, reason("merge_conflict", "Pull request has merge conflicts", "Resolve conflicts and push the updated branch"))
	}
	if pr.MergeState == "BEHIND" {
		reasons = append(reasons, reason("branch_behind", "Branch must be updated with the base branch", "Update the branch and rerun checks"))
	}
	if pr.ReviewDecision == "REVIEW_REQUIRED" {
		reasons = append(reasons, reason("review_required", "Required review approval is missing", "Obtain the required approvals"))
	}
	if pr.ReviewDecision == "CHANGES_REQUESTED" {
		reasons = append(reasons, reason("changes_requested", "A review requests changes", "Address the review and obtain approval"))
	}
	return reasons
}

type mergeCheck struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"details_url"`
	HTMLURL    string `json:"html_url"`
	App        struct {
		Slug string `json:"slug"`
	} `json:"app"`
	Output struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	} `json:"output"`
}

type mergeStatus struct {
	ID          int64  `json:"id"`
	Name        string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description"`
	URL         string `json:"target_url"`
}

func (client Client) mergeChecks(ctx context.Context, repo, sha string) ([]mergeCheck, []mergeStatus, error) {
	var checks []mergeCheck
	for page := 1; ; {
		var response struct {
			Checks []mergeCheck `json:"check_runs"`
		}
		next, err := client.API.Do(ctx, "GET", fmt.Sprintf("repos/%s/commits/%s/check-runs?filter=latest&per_page=100&page=%d", repo, url.PathEscape(sha), page), nil, &response)
		if err != nil {
			return nil, nil, fmt.Errorf("read check runs: %w", err)
		}
		checks = append(checks, response.Checks...)
		if next == 0 {
			break
		}
		if next <= page {
			return nil, nil, errors.New("check pagination did not advance")
		}
		page = next
	}
	// Statuses are historical, newest first. Keep only the latest per context,
	// including contexts beyond the first page.
	var statuses []mergeStatus
	seen := map[string]bool{}
	for page := 1; ; {
		var response []mergeStatus
		next, err := client.API.Do(ctx, "GET", fmt.Sprintf("repos/%s/commits/%s/statuses?per_page=100&page=%d", repo, url.PathEscape(sha), page), nil, &response)
		if err != nil {
			return nil, nil, fmt.Errorf("read commit statuses: %w", err)
		}
		for _, status := range response {
			if !seen[status.Name] {
				seen[status.Name] = true
				statuses = append(statuses, status)
			}
		}
		if next == 0 {
			break
		}
		if next <= page {
			return nil, nil, errors.New("status pagination did not advance")
		}
		page = next
	}
	return checks, statuses, nil
}

func checkReasons(checks []mergeCheck, statuses []mergeStatus) (failed, pending []MergeReason) {
	for _, check := range checks {
		link := check.URL
		if link == "" {
			link = check.HTMLURL
		}
		r := MergeReason{Name: check.Name, URL: link, checkID: check.ID}
		if check.App.Slug == "github-actions" {
			r.jobID = actionsJobID(link)
		}
		if check.Status != "completed" {
			r.Code = "check_pending"
			r.Summary = "Check is " + check.Status
			r.NextAction = "Wait for the check to finish"
			pending = append(pending, r)
			continue
		}
		switch check.Conclusion {
		case "success", "skipped", "neutral":
			continue
		}
		r.Code = "check_failed"
		r.Summary = "Check concluded " + check.Conclusion
		r.NextAction = "Fix the reported failure and rerun checks"
		failed = append(failed, r)
	}
	for _, status := range statuses {
		if status.State == "success" {
			continue
		}
		r := MergeReason{Name: status.Name, URL: status.URL, Summary: compactText(status.Description, 600)}
		if r.Summary == "" {
			r.Summary = "Commit status is " + status.State
		}
		if status.State == "pending" {
			r.Code = "status_pending"
			r.NextAction = "Wait for the status to finish"
			pending = append(pending, r)
		} else {
			r.Code = "status_failed"
			r.NextAction = "Fix the reported failure and rerun the status check"
			failed = append(failed, r)
		}
	}
	if len(checks)+len(statuses) == 0 {
		pending = append(pending, reason("checks_missing", "No checks or commit statuses have been reported", "Configure or start CI for this commit"))
	}
	sort.SliceStable(failed, func(i, j int) bool { return failed[i].Name < failed[j].Name })
	return failed, pending
}

type asyncMerge struct {
	Status  string `json:"status"`
	Details struct {
		UUID    string `json:"uuid"`
		SHA     string `json:"sha"`
		Message string `json:"message"`
	} `json:"details"`
}

// MergePullRequest consumes intermediate states internally and emits one final
// result. Only one merge mutation is ever submitted; accepted requests and queue
// entries are observed until the PR is actually merged.
func (client Client) MergePullRequest(ctx context.Context, repo string, options MergeOptions) (MergeResult, error) {
	result := MergeResult{Status: "error", Repo: repo, Number: options.Number}
	if err := options.Normalize(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	endpoint := fmt.Sprintf("repos/%s/pulls/%d", repo, options.Number)
	var waiting []MergeReason
	finishError := func(err error) (MergeResult, error) {
		if ctx.Err() != nil {
			result.Status = "timeout"
			if errors.Is(ctx.Err(), context.Canceled) {
				result.Status = "cancelled"
			}
			result.Reasons = waiting
			if len(result.Reasons) == 0 {
				result.Reasons = []MergeReason{reason("operation_pending", "The operation did not finish before the deadline", "Check the PR state before retrying")}
			}
			return result, nil
		}
		if result.MergeRequested || result.Queued {
			result.Status = "unknown"
		}
		result.Reasons = []MergeReason{reason("api_error", compactText(err.Error(), 1000), "Check repository access and token permissions; if a merge was requested, check the PR before retrying")}
		return result, err
	}
	for {
		pr, err := client.mergePR(ctx, repo, options.Number)
		if err != nil {
			return finishError(err)
		}
		result.URL = pr.URL
		if pr.Merged {
			if pr.MergeCommit == nil || pr.MergeCommit.OID == "" {
				return finishError(errors.New("merged PR response has no merge SHA"))
			}
			result.Status = "merged"
			result.MergeSHA = pr.MergeCommit.OID
			result.Reasons = nil
			return result, nil
		}
		if result.HeadSHA == "" {
			result.HeadSHA = pr.HeadSHA
		}
		if pr.HeadSHA != result.HeadSHA {
			result.Status = "blocked"
			result.Reasons = []MergeReason{reason("head_changed", "PR head changed while checking or waiting", "Run the command again to check the new commit")}
			return result, nil
		}
		if result.MergeRequested || pr.QueueEntry != nil {
			result.Queued = result.Queued || pr.QueueEntry != nil
			if pr.State != "OPEN" {
				result.Status = "blocked"
				result.Reasons = pr.blockers()
				return result, nil
			}
			if result.RequestID != "" && !result.Queued {
				var state asyncMerge
				if err := client.api(ctx, "GET", endpoint+"/merge-async/"+url.PathEscape(result.RequestID), nil, &state); err != nil {
					return finishError(err)
				}
				switch state.Status {
				case "failed":
					result.Status = "blocked"
					result.Reasons = []MergeReason{reason("merge_rejected", compactText(state.Details.Message, 1000), "Resolve the reported repository rule or merge condition and retry")}
					client.diagnoseMergeRejection(ctx, repo, &result)
					return result, nil
				case "merged":
					if state.Details.SHA == "" {
						return finishError(errors.New("merge result has no merge SHA"))
					}
					result.Status = "merged"
					result.MergeSHA = state.Details.SHA
					result.Reasons = nil
					return result, nil
				case "enqueued":
					result.Queued = true
				case "pending":
				default:
					return finishError(fmt.Errorf("unknown asynchronous merge status %q", state.Status))
				}
			}
			if result.Queued && pr.QueueEntry == nil {
				// Re-read after an enqueue response: the first snapshot may predate it.
				pr, err = client.mergePR(ctx, repo, options.Number)
				if err != nil {
					return finishError(err)
				}
				if pr.Merged {
					continue
				}
				if pr.QueueEntry == nil {
					result.Status = "blocked"
					result.Reasons = []MergeReason{reason("merge_queue_removed", "PR is no longer in the merge queue", "Inspect merge queue checks and repository rules before retrying")}
					client.diagnoseMergeRejection(ctx, repo, &result)
					return result, nil
				}
			}
			waiting = []MergeReason{reason("merge_pending", "Waiting for the accepted merge request or merge queue", "Check the PR state before retrying; the merge may still complete")}
		} else {
			checks, statuses, err := client.prMergeChecks(ctx, repo, pr)
			if err != nil {
				return finishError(err)
			}
			failed, pending := checkReasons(checks, statuses)
			blockers := pr.blockers()
			if len(failed)+len(blockers) > 0 {
				result.Status = "blocked"
				result.Reasons = append(blockers, failed...)
				client.diagnoseChecks(ctx, repo, result.Reasons)
				return result, nil
			}
			waiting = pending
			if pr.Mergeable == "UNKNOWN" || pr.MergeState == "UNKNOWN" {
				waiting = append(waiting, reason("mergeability_pending", "GitHub is calculating mergeability", "Wait for GitHub to calculate merge conditions"))
			}
			if len(waiting) == 0 {
				latest, err := client.mergePR(ctx, repo, options.Number)
				if err != nil {
					return finishError(err)
				}
				if latest.HeadSHA != pr.HeadSHA || latest.BaseSHA != pr.BaseSHA || testSHA(latest) != testSHA(pr) {
					continue
				}
				if latest.Merged {
					continue
				}
				if blockers := latest.blockers(); len(blockers) > 0 {
					result.Status = "blocked"
					result.Reasons = blockers
					return result, nil
				}
				if latest.Mergeable == "UNKNOWN" || latest.MergeState == "UNKNOWN" {
					timer := time.NewTimer(options.Interval)
					waiting = []MergeReason{reason("mergeability_pending", "GitHub is calculating mergeability", "Wait for GitHub to calculate merge conditions")}
					select {
					case <-ctx.Done():
						timer.Stop()
						return finishError(ctx.Err())
					case <-timer.C:
					}
					continue
				}
				var state asyncMerge
				result.MergeRequested = true
				err = client.api(ctx, "PUT", endpoint+"/merge-async", map[string]any{"sha": result.HeadSHA, "merge_method": options.Method, "merge_action": "default", "bypass_rules": false}, &state)
				if err != nil {
					var response *sdk.ErrorResponse
					if errors.As(err, &response) && response.Response != nil && (response.Response.StatusCode == 400 || response.Response.StatusCode == 403 || response.Response.StatusCode == 405 || response.Response.StatusCode == 409 || response.Response.StatusCode == 422) {
						result.Status = "blocked"
						result.Reasons = []MergeReason{reason("merge_rejected", compactText(response.Message, 1000), "Resolve the reported merge condition or permissions; check for an existing merge request before retrying")}
						client.diagnoseMergeRejection(ctx, repo, &result)
						return result, nil
					}
					return finishError(err)
				}
				result.RequestID = state.Details.UUID
				switch state.Status {
				case "merged":
					if state.Details.SHA == "" {
						return finishError(errors.New("merge result has no merge SHA"))
					}
					result.Status = "merged"
					result.MergeSHA = state.Details.SHA
					return result, nil
				case "enqueued":
					result.Queued = true
				case "pending":
					if result.RequestID == "" {
						return finishError(errors.New("accepted merge response has no request UUID"))
					}
				default:
					return finishError(fmt.Errorf("unknown asynchronous merge status %q", state.Status))
				}
				waiting = []MergeReason{reason("merge_pending", "Waiting for the accepted merge request or merge queue", "Check the PR state before retrying; the merge may still complete")}
			}
		}
		timer := time.NewTimer(options.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return finishError(ctx.Err())
		case <-timer.C:
		}
	}
}

func testSHA(pr *mergePR) string {
	if pr.TestCommit != nil {
		return pr.TestCommit.OID
	}
	return ""
}

func (client Client) prMergeChecks(ctx context.Context, repo string, pr *mergePR) ([]mergeCheck, []mergeStatus, error) {
	// GitHub evaluates the test merge commit when it has checks; otherwise use
	// the head commit. Do not reuse results from an earlier head or base.
	if sha := testSHA(pr); sha != "" && sha != pr.HeadSHA {
		checks, statuses, err := client.mergeChecks(ctx, repo, sha)
		if err != nil {
			return nil, nil, err
		}
		if len(checks)+len(statuses) > 0 {
			return checks, statuses, nil
		}
	}
	return client.mergeChecks(ctx, repo, pr.HeadSHA)
}

func (client Client) diagnoseMergeRejection(ctx context.Context, repo string, result *MergeResult) {
	latest, err := client.mergePR(ctx, repo, result.Number)
	if err != nil {
		result.Reasons[0].DiagnosticError = compactText(err.Error(), 500)
		return
	}
	if latest.HeadSHA != result.HeadSHA {
		result.Reasons = append(result.Reasons, reason("head_changed", "PR head changed", "Run the command again to check the new commit"))
		return
	}
	result.Reasons = append(result.Reasons, latest.blockers()...)
	checks, statuses, err := client.prMergeChecks(ctx, repo, latest)
	if err != nil {
		result.Reasons[0].DiagnosticError = compactText(err.Error(), 500)
		return
	}
	failed, pending := checkReasons(checks, statuses)
	result.Reasons = append(result.Reasons, failed...)
	result.Reasons = append(result.Reasons, pending...)
	client.diagnoseChecks(ctx, repo, result.Reasons)
}
