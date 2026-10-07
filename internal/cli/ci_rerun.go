package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
		fmt.Fprintln(stderr, "\nRequests one rerun and waits for the next run_attempt, never the previous result.\nFailed reruns include attempt-specific CI failure evidence. Requires Actions write permission.\nA timeout does not cancel the accepted rerun. Check the run before retrying an unknown outcome.")
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
	if flags.NArg() != 0 {
		return fail(errors.New("unexpected positional arguments"), 2)
	}
	if options.Timeout <= 0 || options.Interval <= 0 || options.MaxLogBytes <= 0 {
		return fail(errors.New("--timeout, --interval and --max-log-bytes must be positive"), 2)
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
	result, err := client.RerunCI(ctx, resolved, options)
	if *jsonOutput {
		if code := encode(out, stderr, result); code != 0 {
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
