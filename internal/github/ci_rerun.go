package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	sdk "github.com/google/go-github/v92/github"
)

type CIRerunOptions struct {
	RunID       int64
	All         bool
	Wait        bool
	DryRun      bool
	Timeout     time.Duration
	Interval    time.Duration
	Annotations bool
	MaxLogBytes int64
	Resume      *CIRerunCheckpoint
	OnRequest   func(CIRerunCheckpoint) error
}

func (options *CIRerunOptions) Normalize() error {
	failures := CIFailureOptions{RunID: options.RunID, MaxLogBytes: options.MaxLogBytes}
	if err := failures.Normalize(); err != nil {
		return err
	}
	options.MaxLogBytes = failures.MaxLogBytes
	if options.Timeout == 0 {
		options.Timeout = 10 * time.Minute
	}
	if options.Interval == 0 {
		options.Interval = 10 * time.Second
	}
	if options.Timeout < 0 || options.Interval < 0 {
		return errors.New("--timeout and --interval must be positive durations")
	}
	return nil
}

type CIRerunResult struct {
	Status           string           `json:"status"`
	Repo             string           `json:"repo"`
	Mode             string           `json:"mode"`
	Run              CIRun            `json:"run"`
	PreviousAttempt  int              `json:"previous_attempt"`
	ExpectedAttempt  int              `json:"expected_attempt"`
	RequestAttempted bool             `json:"request_attempted"`
	RerunRequested   bool             `json:"rerun_requested"`
	Failure          *CIFailureResult `json:"failure,omitempty"`
	Notes            []string         `json:"notes,omitempty"`
	Error            string           `json:"error,omitempty"`
	Resumed          bool             `json:"resumed,omitempty"`
	StateFile        string           `json:"state_file,omitempty"`
}

func (client Client) ciRun(ctx context.Context, repo string, id int64) (CIRun, error) {
	var run CIRun
	if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/actions/runs/%d", repo, id), nil, &run); err != nil {
		return run, fmt.Errorf("read CI run: %w", err)
	}
	if run.ID != id || run.Attempt <= 0 || run.HeadSHA == "" || run.Status == "" || (run.Status == "completed" && run.Conclusion == "") {
		return run, errors.New("GitHub returned incomplete CI run metadata")
	}
	return run, nil
}

// RerunCI sends at most one mutation. Only a newer attempt can satisfy waiting;
// the original completed result is never mistaken for the rerun result.
func (client Client) RerunCI(ctx context.Context, repo string, options CIRerunOptions) (CIRerunResult, error) {
	result := CIRerunResult{Status: "error", Repo: repo, Mode: "failed"}
	if options.All {
		result.Mode = "all"
	}
	if err := options.Normalize(); err != nil {
		result.Error = err.Error()
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	finishError := func(err error) (CIRerunResult, error) {
		result.Error = err.Error()
		if result.RequestAttempted {
			result.Status = "unknown"
			result.Notes = append(result.Notes, "Check the workflow run and attempt before retrying; the rerun may still execute.")
		}
		if ctx.Err() != nil {
			result.Status = "timeout"
			if errors.Is(ctx.Err(), context.Canceled) {
				result.Status = "cancelled"
			}
		}
		return result, err
	}
	if options.Resume != nil {
		state := options.Resume
		if options.DryRun {
			return finishError(errors.New("--resume cannot be combined with --dry-run"))
		}
		if err := state.Validate(repo, options.RunID); err != nil {
			return finishError(err)
		}
		result.Mode, result.Resumed = state.Mode, true
		result.PreviousAttempt, result.ExpectedAttempt = state.PreviousAttempt, state.ExpectedAttempt
		result.RequestAttempted, result.RerunRequested = state.RequestAttempted, state.Accepted
		result.Run = CIRun{ID: state.RunID, HeadSHA: state.HeadSHA}
		return client.waitRerunCI(ctx, repo, options, result)
	}
	run, err := client.ciRun(ctx, repo, options.RunID)
	if err != nil {
		return finishError(err)
	}
	result.Run, result.PreviousAttempt, result.ExpectedAttempt = run, run.Attempt, run.Attempt+1
	if run.Status != "completed" {
		result.Status = "blocked"
		result.Notes = append(result.Notes, "Run is already active; no rerun was requested. Wait for its completion.")
		return result, nil
	}
	if !options.All && !ciUnsuccessful(run.Conclusion) {
		result.Status = "blocked"
		result.Notes = append(result.Notes, "Run has no unsuccessful conclusion; use --all to rerun the entire workflow.")
		return result, nil
	}
	if options.DryRun {
		result.Status = "planned"
		return result, nil
	}
	// Recheck immediately before writing so a concurrent active rerun is preserved.
	current, err := client.ciRun(ctx, repo, options.RunID)
	if err != nil {
		return finishError(err)
	}
	if current.Attempt != run.Attempt || current.HeadSHA != run.HeadSHA || current.Status != "completed" || current.Conclusion != run.Conclusion {
		result.Run, result.Status = current, "blocked"
		result.Notes = append(result.Notes, "Run changed before the rerun request; inspect its current attempt.")
		return result, nil
	}
	endpoint := fmt.Sprintf("repos/%s/actions/runs/%d/rerun-failed-jobs", repo, options.RunID)
	if options.All {
		endpoint = fmt.Sprintf("repos/%s/actions/runs/%d/rerun", repo, options.RunID)
	}
	checkpoint := CIRerunCheckpoint{Version: 1, Repo: repo, RunID: run.ID, PreviousAttempt: run.Attempt, ExpectedAttempt: run.Attempt + 1, HeadSHA: run.HeadSHA, Mode: result.Mode, RequestedAt: time.Now().UTC(), RequestAttempted: true}
	if options.OnRequest != nil {
		if err := options.OnRequest(checkpoint); err != nil {
			return finishError(fmt.Errorf("save rerun checkpoint before request: %w", err))
		}
	}
	result.RequestAttempted = true
	if err := client.api(ctx, "POST", endpoint, map[string]any{}, nil); err != nil {
		var rejection *sdk.ErrorResponse
		if errors.As(err, &rejection) && rejection.Response != nil && rejection.Response.StatusCode >= http.StatusBadRequest && rejection.Response.StatusCode < http.StatusInternalServerError {
			result.Error = err.Error()
			result.Notes = append(result.Notes, "GitHub rejected the rerun request; check Actions write permission and run eligibility.")
			return result, err
		}
		return finishError(err)
	}
	result.RerunRequested, result.Status = true, "requested"
	checkpoint.Accepted = true
	if options.OnRequest != nil {
		if err := options.OnRequest(checkpoint); err != nil {
			return finishError(fmt.Errorf("rerun accepted but checkpoint update failed: %w", err))
		}
	}
	if !options.Wait {
		return result, nil
	}
	return client.waitRerunCI(ctx, repo, options, result)
}

func (client Client) waitRerunCI(ctx context.Context, repo string, options CIRerunOptions, result CIRerunResult) (CIRerunResult, error) {
	expectedHead := result.Run.HeadSHA
	finishError := func(err error) (CIRerunResult, error) {
		result.Error, result.Status = err.Error(), "unknown"
		if ctx.Err() != nil {
			result.Status = "timeout"
			if errors.Is(ctx.Err(), context.Canceled) {
				result.Status = "cancelled"
			}
		}
		return result, err
	}
	for {
		current, err := client.ciRun(ctx, repo, options.RunID)
		if err != nil {
			return finishError(err)
		}
		if current.HeadSHA != expectedHead || current.Attempt < result.PreviousAttempt {
			return finishError(errors.New("workflow identity changed while resuming rerun"))
		}
		if result.Resumed && current.Attempt > result.ExpectedAttempt {
			result.Notes = append(result.Notes, "A newer attempt exists; resumed evidence stays pinned to the saved expected attempt.")
			var pinned CIRun
			if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d", repo, options.RunID, result.ExpectedAttempt), nil, &pinned); err != nil {
				return finishError(err)
			}
			if pinned.ID != options.RunID || pinned.Attempt != result.ExpectedAttempt || pinned.Status == "" || (pinned.Status == "completed" && pinned.Conclusion == "") {
				return finishError(errors.New("saved attempt metadata is incomplete"))
			}
			current = pinned
		}
		result.Run = current
		if current.HeadSHA != expectedHead || current.Attempt < result.PreviousAttempt {
			return finishError(errors.New("workflow identity changed while waiting for rerun"))
		}
		if current.Attempt > result.ExpectedAttempt {
			result.Status = "superseded"
			result.Notes = append(result.Notes, "Another rerun attempt superseded the expected attempt; inspect the workflow history.")
			return result, nil
		}
		if current.Attempt == result.ExpectedAttempt && current.Status == "completed" {
			if current.Conclusion == "success" {
				result.Status = "completed"
				return result, nil
			}
			result.Status = "failed"
			failures, err := client.ciFailuresForRun(ctx, repo, CIFailureOptions{RunID: options.RunID, Annotations: options.Annotations, MaxLogBytes: options.MaxLogBytes}, current)
			if err != nil {
				failures.Status, failures.Complete = "partial", false
				failures.Notes = append(failures.Notes, err.Error())
			}
			result.Failure = &failures
			return result, nil
		}
		if !options.Wait {
			result.Status = "requested"
			if !result.RerunRequested {
				result.Status = "unknown"
			}
			return result, nil
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
