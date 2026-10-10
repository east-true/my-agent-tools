package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

func runCIRerun(ctx context.Context, args []string, out, stderr io.Writer, runner command.Runner, newAPI func(context.Context, command.Runner) (github.API, error)) int {
	flags := flag.NewFlagSet("tools github ci rerun", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub OWNER/REPO (default: current repository)")
	jsonOutput := flags.Bool("json", false, "emit one final result with rerun attempt and failure evidence")
	options := github.CIRerunOptions{}
	resume := flags.Bool("resume", false, "resume observing a saved expected attempt without requesting another rerun")
	statePath := flags.String("state-file", "", "rerun checkpoint path (default: shared repository state)")
	evidenceReader := evidenceReadFlags{}
	evidenceReader.register(flags)
	compact := compactFlags{}
	compact.register(flags, false)
	flags.Int64Var(&options.RunID, "run", 0, "workflow run ID (required; not a PR number)")
	flags.BoolVar(&options.All, "all", false, "rerun the entire workflow instead of failed jobs and their dependents")
	flags.BoolVar(&options.Wait, "wait", true, "wait for the new attempt to complete; --wait=false returns after request acceptance")
	flags.BoolVar(&options.DryRun, "dry-run", false, "validate eligibility and preview without requesting a rerun")
	flags.DurationVar(&options.Timeout, "timeout", 10*time.Minute, "total time limit, including rerun request, waiting and diagnostics")
	flags.DurationVar(&options.Interval, "interval", 10*time.Second, "polling interval")
	flags.BoolVar(&options.Annotations, "annotations", true, "include failure annotations (requires Checks read permission)")
	flags.Int64Var(&options.MaxLogBytes, "max-log-bytes", 8<<20, "maximum downloaded bytes per failed job (1..134217728)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tools github ci rerun --run RUN_ID [options]")
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\nRequests one rerun and waits for the next run_attempt, never the previous result.\nSaves a checkpoint before requesting; --resume only observes the saved expected attempt.\nFailed reruns include attempt-specific CI failure evidence. New requests require Actions write permission.\nA timeout does not cancel the accepted rerun. Use --resume for an unresolved saved request.")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error, code int) int {
		if *jsonOutput {
			_ = encode(out, stderr, map[string]any{"status": "error", "error": err.Error()})
		} else {
			fmt.Fprintln(stderr, "error:", err)
		}
		return code
	}
	if evidenceReader.selected() {
		return evidenceReader.run(flags, out, stderr)
	}
	if flags.NArg() != 0 {
		return fail(errors.New("unexpected positional arguments"), 2)
	}
	if err := compact.validate(); err != nil {
		return fail(err, 2)
	}
	if options.Timeout <= 0 || options.Interval <= 0 || options.MaxLogBytes <= 0 {
		return fail(errors.New("--timeout, --interval and --max-log-bytes must be positive"), 2)
	}
	if *resume && options.DryRun {
		return fail(errors.New("--resume cannot be combined with --dry-run"), 2)
	}
	if err := options.Normalize(); err != nil {
		return fail(err, 2)
	}
	client := github.Client{Runner: runner}
	resolved, err := client.ResolveRepo(ctx, *repo)
	if err != nil {
		return fail(err, 1)
	}
	client.API, err = newAPI(ctx, runner)
	if err != nil {
		return fail(err, 1)
	}
	if *statePath == "" {
		*statePath = defaultRerunState(client.API, resolved, options.RunID)
	}
	var checkpoint *github.CIRerunCheckpoint
	checkpointChanged := *resume
	if *statePath != "" && !options.DryRun {
		*statePath, err = filepath.Abs(*statePath)
		if err != nil {
			return fail(err, 2)
		}
		unlock, err := lockInspectionState(*statePath)
		if err != nil {
			return fail(err, 1)
		}
		defer unlock()
		checkpoint, err = readRerunCheckpoint(*statePath, resolved, options.RunID)
		if err != nil {
			return fail(err, 2)
		}
		if *resume {
			if checkpoint == nil {
				return fail(errors.New("no saved rerun request to resume"), 2)
			}
			conflict := false
			flags.Visit(func(flag *flag.Flag) {
				if flag.Name == "all" && options.All != (checkpoint.Mode == "all") {
					conflict = true
				}
			})
			if conflict {
				return fail(errors.New("--all differs from the saved rerun mode"), 2)
			}
			options.Resume = checkpoint
		} else {
			if checkpoint != nil && !checkpoint.Terminal {
				return fail(errors.New("a saved rerun request is unresolved; use --resume to observe it without sending another request"), 2)
			}
			options.OnRequest = func(state github.CIRerunCheckpoint) error {
				data, err := json.Marshal(state)
				if err != nil {
					return err
				}
				if err := atomicJSONFile(*statePath, data); err != nil {
					return err
				}
				checkpoint = &state
				checkpointChanged = true
				return nil
			}
		}
	} else if *resume {
		return fail(errors.New("--resume requires a saved state file"), 2)
	}
	result, err := client.RerunCI(ctx, resolved, options)
	result.StateFile = *statePath
	if checkpoint != nil && checkpointChanged {
		checkpoint.Terminal = result.Status == "completed" || result.Status == "failed" || result.Status == "superseded" || (result.Status == "error" && result.RequestAttempted)
		checkpoint.Accepted = checkpoint.Accepted || result.RerunRequested
		data, marshalErr := json.Marshal(checkpoint)
		if marshalErr == nil {
			marshalErr = atomicJSONFile(*statePath, data)
		}
		if marshalErr != nil {
			result.Notes = append(result.Notes, "Rerun outcome retained; state update failed: "+marshalErr.Error())
			if err == nil {
				err = marshalErr
			}
		}
	}
	if *jsonOutput || compact.Enabled {
		if code := encodeCompact(out, stderr, result, compact); code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(out, "%s: %s run=%d attempt=%d expected=%d mode=%s\n%s\nrerun requested: %t; request attempted: %t\n", result.Status, result.Repo, result.Run.ID, result.Run.Attempt, result.ExpectedAttempt, result.Mode, result.Run.URL, result.RerunRequested, result.RequestAttempted)
		if result.Error != "" {
			fmt.Fprintln(out, "error:", result.Error)
		}
		for _, note := range result.Notes {
			fmt.Fprintln(out, "note:", note)
		}
		if result.Failure != nil {
			if code := encode(out, stderr, result.Failure); code != 0 {
				return code
			}
		}
	}
	if err != nil || (result.Status != "completed" && result.Status != "requested" && result.Status != "planned") {
		return 1
	}
	return 0
}
