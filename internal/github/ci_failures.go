package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type CIFailureOptions struct {
	RunID       int64
	MaxLogBytes int64
	Annotations bool
}

func (options *CIFailureOptions) Normalize() error {
	if options.RunID <= 0 {
		return errors.New("--run must be a positive workflow run ID")
	}
	if options.MaxLogBytes == 0 {
		options.MaxLogBytes = 8 << 20
	}
	if options.MaxLogBytes < 1 || options.MaxLogBytes > 128<<20 {
		return errors.New("--max-log-bytes must be between 1 and 134217728")
	}
	return nil
}

type CIRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Attempt    int    `json:"run_attempt"`
	HeadSHA    string `json:"head_sha"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"html_url"`
}

type CIStep struct {
	Number      int    `json:"number"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

type CIAnnotation struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Level     string `json:"annotation_level"`
	Title     string `json:"title,omitempty"`
	Message   string `json:"message"`
	Details   string `json:"raw_details,omitempty"`
}

type CIJob struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Conclusion  string         `json:"conclusion"`
	URL         string         `json:"html_url"`
	CheckRunURL string         `json:"check_run_url,omitempty"`
	Steps       []CIStep       `json:"steps"`
	Annotations []CIAnnotation `json:"annotations"`
	Notes       []string       `json:"notes,omitempty"`
}

type CIFailureResult struct {
	Status   string       `json:"status"`
	Repo     string       `json:"repo"`
	Run      CIRun        `json:"run"`
	Complete bool         `json:"complete"`
	Jobs     []CIJob      `json:"failed_jobs"`
	Evidence []CIEvidence `json:"evidence"`
	Notes    []string     `json:"notes,omitempty"`
}

type CIJobLogAPI interface {
	CIJobLog(context.Context, string, int64, int64) (string, bool, error)
}

func ciUnsuccessful(conclusion string) bool {
	switch conclusion {
	case "failure", "timed_out", "cancelled", "action_required", "startup_failure", "stale":
		return true
	}
	return false
}

// CIFailures collects an explicitly selected run attempt. It never waits,
// modifies a PR, retries a workflow, or invokes a model.
func (client Client) CIFailures(ctx context.Context, repo string, options CIFailureOptions) (CIFailureResult, error) {
	result := CIFailureResult{Status: "ok", Repo: repo, Complete: true, Jobs: []CIJob{}, Evidence: []CIEvidence{}}
	if err := options.Normalize(); err != nil {
		return result, err
	}
	if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/actions/runs/%d", repo, options.RunID), nil, &result.Run); err != nil {
		return result, fmt.Errorf("read CI run: %w", err)
	}
	if result.Run.ID != options.RunID || result.Run.Attempt <= 0 || result.Run.HeadSHA == "" || result.Run.Status == "" {
		return result, errors.New("GitHub returned incomplete CI run metadata")
	}
	return client.ciFailuresForRun(ctx, repo, options, result.Run)
}

// ciFailuresForRun pins diagnostics to the observed attempt, even when another
// rerun starts while failure evidence is being downloaded.
func (client Client) collectCIFailuresForRun(ctx context.Context, repo string, options CIFailureOptions, run CIRun) (CIFailureResult, error) {
	result := CIFailureResult{Status: "ok", Repo: repo, Run: run, Complete: true, Jobs: []CIJob{}, Evidence: []CIEvidence{}}
	seenPages := map[int]bool{}
	for page := 1; ; {
		seenPages[page] = true
		var response struct {
			Jobs []CIJob `json:"jobs"`
		}
		next, err := client.API.Do(ctx, "GET", fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100&page=%d", repo, options.RunID, result.Run.Attempt, page), nil, &response)
		if err != nil {
			if len(result.Jobs) > 0 {
				result.Complete = false
				result.Notes = append(result.Notes, "Remaining job pages unavailable: "+err.Error())
				break
			}
			return result, fmt.Errorf("read CI jobs: %w", err)
		}
		for _, job := range response.Jobs {
			if job.Status != "completed" {
				result.Complete = false
				result.Notes = append(result.Notes, fmt.Sprintf("Job %d is not completed; this is a snapshot.", job.ID))
			}
			if ciUnsuccessful(job.Conclusion) {
				if job.ID <= 0 {
					return result, errors.New("GitHub returned a failed CI job without an ID")
				}
				job.Annotations = []CIAnnotation{}
				failedSteps := []CIStep{}
				for _, step := range job.Steps {
					if ciUnsuccessful(step.Conclusion) {
						failedSteps = append(failedSteps, step)
					}
				}
				job.Steps = failedSteps
				result.Jobs = append(result.Jobs, job)
			}
		}
		if next == 0 {
			break
		}
		if next < 0 || seenPages[next] {
			return result, errors.New("CI jobs pagination returned a repeated page")
		}
		page = next
	}
	if result.Run.Status != "completed" {
		result.Complete = false
		result.Notes = append(result.Notes, "Run is still in progress; this is a snapshot, not a final CI result.")
	}
	if ciUnsuccessful(result.Run.Conclusion) && len(result.Jobs) == 0 {
		result.Complete = false
		result.Notes = append(result.Notes, "Run is unsuccessful but no failed jobs were returned; inspect the workflow run page.")
	}
	logAPI, supportsLogs := client.API.(CIJobLogAPI)
	for i := range result.Jobs {
		job := &result.Jobs[i]
		if options.Annotations && job.CheckRunURL != "" {
			annotations, err := client.ciAnnotations(ctx, repo, job.CheckRunURL)
			job.Annotations = annotations
			if err != nil {
				result.Complete = false
				job.Notes = append(job.Notes, "Annotations unavailable: "+err.Error())
			}
		}
		if !supportsLogs {
			result.Complete = false
			job.Notes = append(job.Notes, "GitHub API does not support CI job log downloads.")
			continue
		}
		log, truncated, err := logAPI.CIJobLog(ctx, repo, job.ID, options.MaxLogBytes)
		if err != nil {
			result.Complete = false
			job.Notes = append(job.Notes, "Logs unavailable: "+err.Error())
			continue
		}
		if truncated {
			result.Complete = false
			job.Notes = append(job.Notes, "Log exceeds --max-log-bytes; returned evidence is truncated. Increase the limit or inspect the job page.")
		}
		if strings.TrimSpace(log) == "" {
			result.Complete = false
			job.Notes = append(job.Notes, "Job log is empty; no failure evidence can be established.")
			continue
		}
		for _, section := range ciLogSections(*job, log) {
			evidence := ciExtractEvidence(section.lines, truncated)
			if section.step.Number == 0 {
				kind := "log"
				if len(evidence.Tests) > 0 {
					kind = "test_failure"
				}
				evidence = CIEvidence{Kind: kind, Tests: evidence.Tests, Truncated: truncated, Lines: section.lines, Notice: "Step boundaries are unavailable; full available job log retained."}
			}
			evidence.Occurrences = []CIOccurrence{{JobID: job.ID, JobName: job.Name, StepNumber: section.step.Number, StepName: section.step.Name, URL: job.URL, StartLine: section.startLine, EndLine: section.startLine + len(section.lines) - 1}}
			ciAppendEvidence(&result.Evidence, evidence)
		}
	}
	if !result.Complete {
		result.Status = "partial"
	}
	return result, nil
}

func ciCheckRunID(repo, checkURL string) (int64, error) {
	u, err := url.Parse(checkURL)
	prefix := "/repos/" + repo + "/check-runs/"
	if err != nil || !strings.HasPrefix(u.Path, prefix) {
		return 0, errors.New("invalid check_run_url")
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(u.Path, prefix), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid check run ID")
	}
	return id, nil
}

func (client Client) ciAnnotations(ctx context.Context, repo, checkURL string) ([]CIAnnotation, error) {
	id, err := ciCheckRunID(repo, checkURL)
	if err != nil {
		return nil, err
	}
	all := []CIAnnotation{}
	seen := map[int]bool{}
	for page := 1; ; {
		seen[page] = true
		var annotations []CIAnnotation
		next, err := client.API.Do(ctx, "GET", fmt.Sprintf("repos/%s/check-runs/%d/annotations?per_page=100&page=%d", repo, id, page), nil, &annotations)
		if err != nil {
			return all, err
		}
		for _, annotation := range annotations {
			if annotation.Level == "failure" || annotation.Level == "warning" {
				all = append(all, annotation)
			}
		}
		if next == 0 {
			return all, nil
		}
		if next < 0 || seen[next] {
			return all, errors.New("CI annotations pagination returned a repeated page")
		}
		page = next
	}
}

type ciLogSection struct {
	step      CIStep
	startLine int
	lines     []string
}

// Use step timestamps only when every failed step and every line can be mapped.
// Otherwise retain the full job log, rather than guessing step boundaries.
func ciLogSections(job CIJob, log string) []ciLogSection {
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(log, "\r\n", "\n"), "\n"), "\n")
	fallback := []ciLogSection{{step: CIStep{Name: "unknown (full job log)"}, startLine: 1, lines: lines}}
	var sections []ciLogSection
	for _, step := range job.Steps {
		if !ciUnsuccessful(step.Conclusion) {
			continue
		}
		start, err1 := time.Parse(time.RFC3339Nano, step.StartedAt)
		end, err2 := time.Parse(time.RFC3339Nano, step.CompletedAt)
		if err1 != nil || err2 != nil || end.Before(start) {
			return fallback
		}
		// GitHub step metadata can have second precision while logs use fractions.
		if !strings.Contains(strings.Split(step.CompletedAt, "Z")[0], ".") {
			end = end.Add(time.Second - time.Nanosecond)
		}
		first, last := -1, -1
		var previous time.Time
		for i, line := range lines {
			stamp, _, ok := strings.Cut(strings.TrimPrefix(line, "\ufeff"), " ")
			timestamp, err := time.Parse(time.RFC3339Nano, stamp)
			if !ok || err != nil || (!previous.IsZero() && timestamp.Before(previous)) {
				return fallback
			}
			previous = timestamp
			if !timestamp.Before(start) && !timestamp.After(end) {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first < 0 {
			return fallback
		}
		sections = append(sections, ciLogSection{step: step, startLine: first + 1, lines: lines[first : last+1]})
	}
	if len(sections) == 0 {
		return fallback
	}
	return sections
}
