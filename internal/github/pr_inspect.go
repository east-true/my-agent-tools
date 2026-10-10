package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type InspectOptions struct {
	Number         int
	Wait           bool
	Timeout        time.Duration
	Interval       time.Duration
	Annotations    bool
	MaxLogBytes    int64
	Sections       string
	Conversation   bool
	CachedFailures []CIFailureResult
	CachedHead     string
	CachedBase     string
	CachedComments map[string]ReviewComment
}

func (options *InspectOptions) Normalize() error {
	merge := MergeOptions{Number: options.Number, Timeout: options.Timeout, Interval: options.Interval}
	if err := merge.Normalize(); err != nil {
		return err
	}
	failure := CIFailureOptions{RunID: 1, MaxLogBytes: options.MaxLogBytes}
	if err := failure.Normalize(); err != nil {
		return err
	}
	options.Timeout, options.Interval, options.MaxLogBytes = merge.Timeout, merge.Interval, failure.MaxLogBytes
	if options.Sections == "" || options.Sections == "all" {
		options.Sections = "checks,failures,reviews"
	}
	selected := map[string]bool{}
	for _, section := range strings.Split(options.Sections, ",") {
		if section != "checks" && section != "failures" && section != "reviews" {
			return errors.New("--sections must contain checks, failures or reviews")
		}
		selected[section] = true
	}
	ordered := []string{}
	for section := range selected {
		ordered = append(ordered, section)
	}
	sort.Strings(ordered)
	options.Sections = strings.Join(ordered, ",")
	if options.Conversation && !options.includes("reviews") {
		return errors.New("--conversation requires the reviews section")
	}
	if options.Wait && !options.includes("checks") && !options.includes("failures") {
		return errors.New("--wait requires the checks or failures section")
	}
	return nil
}

func (options InspectOptions) includes(section string) bool {
	for _, item := range strings.Split(options.Sections, ",") {
		if item == section {
			return true
		}
	}
	return false
}

type PRState struct {
	URL            string `json:"url"`
	State          string `json:"state"`
	Draft          bool   `json:"draft"`
	HeadSHA        string `json:"head_sha"`
	BaseSHA        string `json:"base_sha"`
	CheckSHA       string `json:"check_sha"`
	MergeState     string `json:"merge_state"`
	ReviewDecision string `json:"review_decision"`
}

type PRCheck struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url,omitempty"`
	RunID      int64  `json:"run_id,omitempty"`
}

type InspectResult struct {
	Status         string            `json:"status"`
	Repo           string            `json:"repo"`
	Number         int               `json:"number"`
	Complete       bool              `json:"complete"`
	PR             PRState           `json:"pr"`
	Checks         []PRCheck         `json:"checks"`
	Reviews        *ReviewResult     `json:"reviews,omitempty"`
	Runs           []CIRun           `json:"runs"`
	Failures       []CIFailureResult `json:"failures"`
	Reasons        []MergeReason     `json:"reasons,omitempty"`
	Notes          []string          `json:"notes,omitempty"`
	Sections       string            `json:"sections"`
	ReusedEvidence int               `json:"reused_evidence,omitempty"`
}

func actionsRunID(repo, link string) int64 {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" {
		return 0
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || !strings.EqualFold(parts[0]+"/"+parts[1], repo) || parts[2] != "actions" || parts[3] != "runs" {
		return 0
	}
	id, _ := strconv.ParseInt(parts[4], 10, 64)
	if id <= 0 {
		return 0
	}
	return id
}

// InspectPR composes read-only metadata, checks, reviews and exact-attempt
// failure collection. Waiting consumes intermediate snapshots internally.
func (client Client) InspectPR(ctx context.Context, repo string, options InspectOptions) (InspectResult, error) {
	result := InspectResult{Status: "error", Repo: repo, Number: options.Number, Complete: true, Checks: []PRCheck{}, Runs: []CIRun{}, Failures: []CIFailureResult{}}
	if err := options.Normalize(); err != nil {
		return result, err
	}
	result.Sections = options.Sections
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	partial := func(err error) {
		result.Complete, result.Status = false, "partial"
		result.Notes = append(result.Notes, err.Error())
	}
	var pr *mergePR
	var checks []mergeCheck
	var statuses []mergeStatus
	var failed, pending []MergeReason
	var initialHead string
	for {
		var err error
		pr, err = client.mergePR(ctx, repo, options.Number)
		if err != nil {
			return result, err
		}
		if initialHead == "" {
			initialHead = pr.HeadSHA
		} else if initialHead != pr.HeadSHA {
			partial(errors.New("PR head changed while waiting; inspect the new commit again"))
			return result, nil
		}
		checkSHA := pr.HeadSHA
		if pr.TestCommit != nil && pr.TestCommit.OID != "" {
			checkSHA = pr.TestCommit.OID
		}
		if options.includes("checks") || options.includes("failures") {
			checks, statuses, err = client.mergeChecks(ctx, repo, checkSHA)
		}
		if err != nil {
			return result, err
		}
		if (options.includes("checks") || options.includes("failures")) && len(checks)+len(statuses) == 0 && checkSHA != pr.HeadSHA {
			checkSHA = pr.HeadSHA
			checks, statuses, err = client.mergeChecks(ctx, repo, checkSHA)
			if err != nil {
				return result, err
			}
		}
		result.PR = PRState{URL: pr.URL, State: pr.State, Draft: pr.IsDraft, HeadSHA: pr.HeadSHA, BaseSHA: pr.BaseSHA, CheckSHA: checkSHA, MergeState: pr.MergeState, ReviewDecision: pr.ReviewDecision}
		if options.includes("checks") || options.includes("failures") {
			failed, pending = checkReasons(checks, statuses)
		}
		if !options.Wait || len(pending) == 0 || len(failed) > 0 || pr.State != "OPEN" {
			break
		}
		timer := time.NewTimer(options.Interval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			result.Status, result.Complete = "timeout", false
			if errors.Is(ctx.Err(), context.Canceled) {
				result.Status = "cancelled"
			}
			result.Reasons = pending
			return result, nil
		}
	}
	result.Status = "ready"
	if pr.Merged {
		result.Status = "merged"
	} else {
		result.Reasons = append(result.Reasons, pr.blockers()...)
		if pr.Mergeable == "UNKNOWN" || pr.MergeState == "UNKNOWN" {
			pending = append(pending, reason("mergeability_pending", "GitHub is calculating mergeability", "Inspect again after GitHub finishes"))
		}
		result.Reasons = append(result.Reasons, failed...)
		result.Reasons = append(result.Reasons, pending...)
		if len(pr.blockers())+len(failed) > 0 || pr.MergeState == "BLOCKED" {
			result.Status = "blocked"
			if len(result.Reasons) == 0 {
				result.Reasons = append(result.Reasons, reason("repository_rules", "GitHub reports blocked merge conditions", "Inspect repository rules and required conversations"))
			}
		} else if len(pending) > 0 {
			result.Status = "pending"
		}
	}
	// Check descriptions are already in the snapshot. CI jobs are downloaded
	// once below, rather than first downloading a second diagnostic log.
	for i := range failed {
		for _, check := range checks {
			if failed[i].checkID == check.ID {
				failed[i].Details = compactText(strings.Join([]string{check.Output.Title, check.Output.Summary, check.Output.Text}, "\n"), 1500)
			}
		}
	}
	for i := range result.Reasons {
		for _, detail := range failed {
			if result.Reasons[i].Code == detail.Code && result.Reasons[i].Name == detail.Name {
				result.Reasons[i] = detail
			}
		}
	}
	runIDs := map[int64]bool{}
	for _, check := range checks {
		if check.ID <= 0 || check.Name == "" || check.Status == "" || (check.Status == "completed" && check.Conclusion == "") {
			partial(errors.New("GitHub returned incomplete check identity or state"))
		}
		link := check.URL
		if link == "" {
			link = check.HTMLURL
		}
		id := int64(0)
		if check.App.Slug == "github-actions" {
			id = actionsRunID(repo, link)
		}
		if id != 0 && options.includes("failures") {
			runIDs[id] = true
		}
		if options.includes("checks") {
			result.Checks = append(result.Checks, PRCheck{ID: check.ID, Kind: "check", Name: check.Name, Status: check.Status, Conclusion: check.Conclusion, URL: link, RunID: id})
		}
	}
	for _, status := range statuses {
		if status.ID <= 0 || status.Name == "" || status.State == "" {
			partial(errors.New("GitHub returned incomplete commit status identity or state"))
		}
		if options.includes("checks") {
			result.Checks = append(result.Checks, PRCheck{ID: status.ID, Kind: "status", Name: status.Name, Status: status.State, URL: status.URL})
		}
	}
	sort.Slice(result.Checks, func(i, j int) bool {
		return result.Checks[i].Kind+result.Checks[i].Name < result.Checks[j].Kind+result.Checks[j].Name
	})
	ids := []int64{}
	for id := range runIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		run, err := client.ciRun(ctx, repo, id)
		if err != nil {
			partial(err)
			continue
		}
		if run.HeadSHA != pr.HeadSHA && run.HeadSHA != result.PR.CheckSHA {
			partial(fmt.Errorf("run %d is for a different commit; failure evidence omitted", id))
			continue
		}
		result.Runs = append(result.Runs, run)
		if run.Status == "completed" && ciUnsuccessful(run.Conclusion) {
			cached := false
			for _, failure := range options.CachedFailures {
				if failure.Complete && strings.EqualFold(failure.Repo, repo) && failure.Run.ID == run.ID && failure.Run.Attempt == run.Attempt && failure.Run.HeadSHA == run.HeadSHA && failure.Run.Status == run.Status && failure.Run.Conclusion == run.Conclusion {
					result.Failures = append(result.Failures, failure)
					result.ReusedEvidence++
					cached = true
					break
				}
			}
			if cached {
				continue
			}
			failure, err := client.ciFailuresForRun(ctx, repo, CIFailureOptions{RunID: id, MaxLogBytes: options.MaxLogBytes, Annotations: options.Annotations}, run)
			if err != nil {
				failure.Status, failure.Complete = "partial", false
				failure.Notes = append(failure.Notes, err.Error())
			}
			result.Failures = append(result.Failures, failure)
			if !failure.Complete {
				partial(fmt.Errorf("run %d failure evidence is incomplete", id))
			}
		}
	}
	if options.includes("reviews") {
		known := options.CachedComments
		if options.CachedBase != pr.BaseSHA {
			known = nil
		}
		reviews, err := client.PullRequestReviews(ctx, repo, ReviewOptions{Number: options.Number, Conversation: options.Conversation, CachedHead: options.CachedHead, CachedComments: known})
		if err != nil {
			partial(err)
		} else {
			result.Reviews = &reviews
			if !reviews.Complete || reviews.HeadSHA != pr.HeadSHA || reviews.ReviewDecision != pr.ReviewDecision {
				partial(errors.New("review collection is incomplete or belongs to a different PR head"))
			}
			for _, review := range reviews.Reviews {
				if review.ID <= 0 {
					partial(errors.New("submitted review lacks an identity"))
				}
			}
			for _, thread := range reviews.Threads {
				for _, comment := range thread.Comments {
					if comment.ID == "" {
						partial(errors.New("review comment lacks an identity"))
					}
				}
			}
			if len(reviews.Threads) > 0 && result.Status == "ready" {
				result.Status = "blocked"
				result.Reasons = append(result.Reasons, reason("unresolved_reviews", "Unresolved review threads are present", "Read the included threads and address the requests"))
			}
		}
	}
	current, err := client.mergePR(ctx, repo, options.Number)
	if err != nil {
		partial(err)
	} else if current.HeadSHA != pr.HeadSHA || current.BaseSHA != pr.BaseSHA || current.ReviewDecision != pr.ReviewDecision || current.MergeState != pr.MergeState || current.State != pr.State || current.IsDraft != pr.IsDraft || current.Mergeable != pr.Mergeable || inspectTestSHA(current) != inspectTestSHA(pr) {
		partial(errors.New("PR head, base or review/merge conditions changed during collection; inspect again"))
	}
	return result, nil
}

func inspectTestSHA(pr *mergePR) string {
	if pr.TestCommit != nil {
		return pr.TestCommit.OID
	}
	return ""
}
