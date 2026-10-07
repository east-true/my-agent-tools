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

func runPRMerge(ctx context.Context, args []string, out, stderr io.Writer, runner command.Runner, newAPI func(context.Context, command.Runner) (github.API, error)) int {
	flags := flag.NewFlagSet("tools github pr merge", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub OWNER/REPO (default: current repository)")
	jsonOutput := flags.Bool("json", false, "emit one final structured result")
	options := github.MergeOptions{}
	flags.IntVar(&options.Number, "number", 0, "pull request number (required)")
	flags.DurationVar(&options.Timeout, "timeout", 10*time.Minute, "maximum total time for checks and merge completion")
	flags.DurationVar(&options.Interval, "interval", 10*time.Second, "polling interval")
	flags.StringVar(&options.Method, "method", "squash", "merge, squash, or rebase (direct merges; merge queues use repository settings)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tools github pr merge --number NUMBER [options]")
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\nWaits internally for all reported checks, then merges the checked SHA without bypassing repository rules.\nReturns one final result with failure evidence or pending items. No separate check or wait command is needed.\nMissing checks wait until timeout; skipped and neutral check conclusions follow GitHub's success rules.\nAn accepted merge request may still complete after timeout; inspect merge_requested and request_id before retrying.")
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
	if options.Timeout <= 0 || options.Interval <= 0 {
		return fail(errors.New("--timeout and --interval must be positive durations"), 2)
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
	result, err := client.MergePullRequest(ctx, resolved, options)
	code := 0
	if err != nil || result.Status != "merged" {
		code = 1
	}
	if *jsonOutput {
		if encode(out, stderr, result) != 0 {
			return 1
		}
		return code
	}
	fmt.Fprintf(out, "%s: PR #%d %s\n", result.Status, result.Number, result.URL)
	if result.MergeSHA != "" {
		fmt.Fprintln(out, "merge SHA:", result.MergeSHA)
	}
	if result.MergeRequested {
		fmt.Fprintf(out, "merge requested: true; request ID: %s; queued: %t\n", result.RequestID, result.Queued)
	}
	for _, r := range result.Reasons {
		fmt.Fprintf(out, "[%s] %s: %s\n", r.Code, r.Name, r.Summary)
		if r.Details != "" {
			fmt.Fprintln(out, r.Details)
		}
		if r.DiagnosticError != "" {
			fmt.Fprintln(out, "diagnostic error:", r.DiagnosticError)
		}
		if r.URL != "" {
			fmt.Fprintln(out, r.URL)
		}
		fmt.Fprintln(out, "next:", r.NextAction)
	}
	return code
}
