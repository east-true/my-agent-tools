package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

func actionsJobID(link string) int64 {
	u, err := url.Parse(link)
	if err != nil || u.Host != "github.com" {
		return 0
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 7 || parts[2] != "actions" || parts[3] != "runs" || parts[5] != "job" {
		return 0
	}
	id, err := strconv.ParseInt(parts[6], 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

var terminalEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func compactText(value string, limit int) string {
	value = terminalEscape.ReplaceAllString(value, "")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}

// CheckFailureEvidence keeps check output and external-CI annotations intact.
// Actions job annotations/log facts are stored once in MergeResult.Failures.
type CheckFailureEvidence struct {
	ID          int64          `json:"id"`
	Complete    bool           `json:"complete"`
	Output      CheckOutput    `json:"output"`
	Annotations []CIAnnotation `json:"annotations,omitempty"`
}

type CheckOutput struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Text    string `json:"text"`
}

type mergeDiagnosticJob struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"run_id"`
	Attempt     int    `json:"run_attempt"`
	HeadSHA     string `json:"head_sha"`
	CheckRunURL string `json:"check_run_url"`
}

func mergeDiagnosticError(reason *MergeReason, err error) {
	reason.Check.Complete = false
	reason.DiagnosticError = compactText(strings.TrimSpace(reason.DiagnosticError+"\n"+err.Error()), 1000)
}

// diagnoseChecks uses the same exact-attempt collector as ci failures, rerun and
// inspect. Job metadata identifies the historical attempt even after a rerun.
func (client Client) diagnoseChecks(ctx context.Context, repo string, pr *mergePR, checks []mergeCheck, options MergeOptions, result *MergeResult) {
	snapshots := map[int64]mergeCheck{}
	for _, check := range checks {
		snapshots[check.ID] = check
	}
	type collection struct {
		result CIFailureResult
		err    error
	}
	collected := map[string]*collection{}
	ordered := []*collection{}
	for i := range result.Reasons {
		reason := &result.Reasons[i]
		if reason.Code != "check_failed" || reason.checkID <= 0 {
			continue
		}
		check := snapshots[reason.checkID]
		reason.Check = &CheckFailureEvidence{ID: reason.checkID, Complete: true, Output: check.Output}
		reason.Details = compactText(strings.Join([]string{check.Output.Title, check.Output.Summary, check.Output.Text}, "\n"), 1000)
		matched := false
		if check.App.Slug == "github-actions" {
			runID := actionsRunID(repo, reason.URL)
			if runID == 0 || reason.jobID == 0 {
				mergeDiagnosticError(reason, errors.New("Actions check has no verified run/job link; workflow evidence omitted"))
			} else {
				var job mergeDiagnosticJob
				err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/actions/jobs/%d", repo, reason.jobID), nil, &job)
				checkID, checkErr := ciCheckRunID(repo, job.CheckRunURL)
				if err == nil && (job.ID != reason.jobID || job.RunID != runID || job.Attempt <= 0 || job.HeadSHA == "" || (job.HeadSHA != pr.HeadSHA && job.HeadSHA != testSHA(pr)) || checkErr != nil || checkID != reason.checkID) {
					err = errors.New("Actions job identity, commit or check does not match the failed PR check; workflow evidence omitted")
				}
				if err != nil {
					mergeDiagnosticError(reason, err)
				} else {
					key := fmt.Sprintf("%d:%d", job.RunID, job.Attempt)
					cached, exists := collected[key]
					if !exists {
						cached = &collection{}
						cached.result, cached.err = client.mergeFailureForJob(ctx, repo, pr, job, options)
						collected[key] = cached
						ordered = append(ordered, cached)
					}
					if cached.result.Run.ID != 0 {
						reason.RunID, reason.RunAttempt = job.RunID, job.Attempt
					}
					if cached.err != nil {
						mergeDiagnosticError(reason, cached.err)
					} else {
						for _, failedJob := range cached.result.Jobs {
							checkID, checkErr := ciCheckRunID(repo, failedJob.CheckRunURL)
							if failedJob.ID == job.ID && checkErr == nil && checkID == reason.checkID {
								matched = true
								reason.Details = compactText(reason.Details+"\n"+mergeFailureSummary(cached.result, failedJob), 2500)
								break
							}
						}
						if !matched {
							missing := errors.New("Selected failed job/check is missing from its run attempt; inspect the workflow")
							cached.result.Complete, cached.result.Status = false, "partial"
							cached.result.Notes = append(cached.result.Notes, missing.Error())
							mergeDiagnosticError(reason, missing)
						}
						if !cached.result.Complete {
							notes := append([]string{}, cached.result.Notes...)
							for _, failedJob := range cached.result.Jobs {
								notes = append(notes, failedJob.Notes...)
							}
							mergeDiagnosticError(reason, fmt.Errorf("CI failure evidence is partial: %s", strings.Join(notes, "; ")))
						}
					}
				}
			}
		}
		// External CI and unavailable Actions diagnostics retain all check
		// annotations. When Actions collected them, do not fetch them twice.
		if options.Annotations && !matched {
			annotations, err := client.ciAnnotations(ctx, repo, fmt.Sprintf("https://api.github.com/repos/%s/check-runs/%d", repo, reason.checkID))
			reason.Check.Annotations = annotations
			if err != nil {
				mergeDiagnosticError(reason, err)
			}
			for _, annotation := range annotations[:min(3, len(annotations))] {
				reason.Details = compactText(reason.Details+fmt.Sprintf("\n%s:%d: %s", annotation.Path, annotation.StartLine, annotation.Message), 2500)
			}
		}
		if reason.Details == "" {
			reason.Details = "No detailed failure evidence was available; inspect the linked check."
		}
	}
	for _, cached := range ordered {
		if cached.result.Run.ID != 0 {
			result.Failures = append(result.Failures, cached.result)
		}
	}
}

func (client Client) mergeFailureForJob(ctx context.Context, repo string, pr *mergePR, job mergeDiagnosticJob, options MergeOptions) (CIFailureResult, error) {
	var run CIRun
	if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d", repo, job.RunID, job.Attempt), nil, &run); err != nil {
		return CIFailureResult{}, err
	}
	if run.ID != job.RunID || run.Attempt != job.Attempt || run.HeadSHA != job.HeadSHA || (run.HeadSHA != pr.HeadSHA && run.HeadSHA != testSHA(pr)) || run.Status == "" || (run.Status == "completed" && run.Conclusion == "") {
		return CIFailureResult{}, errors.New("Actions run attempt identity or commit changed; workflow evidence omitted")
	}
	failures, err := client.ciFailuresForRun(ctx, repo, CIFailureOptions{RunID: run.ID, Annotations: options.Annotations, MaxLogBytes: options.MaxLogBytes}, run)
	if err != nil {
		failures.Status, failures.Complete = "partial", false
		failures.Notes = append(failures.Notes, err.Error())
	}
	return failures, err
}

// The readable reason is bounded; the complete structured result above is not.
func mergeFailureSummary(failure CIFailureResult, job CIJob) string {
	parts := []string{}
	for _, annotation := range job.Annotations[:min(3, len(job.Annotations))] {
		parts = append(parts, fmt.Sprintf("%s:%d: %s", annotation.Path, annotation.StartLine, annotation.Message))
	}
	for _, step := range job.Steps[:min(3, len(job.Steps))] {
		parts = append(parts, "Failed step: "+step.Name)
	}
	for _, evidence := range failure.Evidence {
		belongs := false
		for _, occurrence := range evidence.Occurrences {
			belongs = belongs || occurrence.JobID == job.ID
		}
		if !belongs {
			continue
		}
		for _, diagnostic := range evidence.Diagnostics[:min(3, len(evidence.Diagnostics))] {
			parts = append(parts, fmt.Sprintf("%s:%d:%d: %s", diagnostic.Path, diagnostic.Line, diagnostic.Column, diagnostic.Message))
		}
		for _, test := range evidence.Tests[:min(3, len(evidence.Tests))] {
			parts = append(parts, "Failed test: "+test.Path+" "+test.Name+" "+test.Message)
		}
		found := 0
		for _, line := range evidence.Lines {
			if ciError.MatchString(ciNormalizedLine(line)) {
				parts = append(parts, line)
				found++
				if found == 3 {
					break
				}
			}
		}
		if len(parts) >= 12 {
			break
		}
	}
	return compactText(strings.Join(parts, "\n"), 1500)
}
