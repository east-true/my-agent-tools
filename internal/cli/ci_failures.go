package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

func runCIFailures(ctx context.Context, args []string, out, stderr io.Writer, runner command.Runner, newAPI func(context.Context, command.Runner) (github.API, error)) int {
	if len(args) > 0 && args[0] == "rerun" {
		return runCIRerun(ctx, args[1:], out, stderr, runner, newAPI)
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, "Usage:\n  tools github ci failures --run RUN_ID [--repo OWNER/REPO] [--json]\n  tools github ci rerun --run RUN_ID [--all] [--wait=false] [--json]\n\nfailures collects jobs, steps, annotations and compiler facts (read-only).\nrerun retries failed jobs by default and waits for a new attempt's result.\nUse '<command> --help' for options.")
		return 0
	}
	if args[0] != "failures" {
		fmt.Fprintf(stderr, "unknown ci action %q\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("tools github ci failures", flag.ContinueOnError)
	flags.SetOutput(stderr)
	compact := compactFlags{}
	compact.register(flags, false)
	repo := flags.String("repo", "", "GitHub OWNER/REPO (default: current repository)")
	runID := flags.Int64("run", 0, "workflow run ID (required; not a PR number)")
	jsonOutput := flags.Bool("json", false, "emit structured evidence and per-job notes")
	annotations := flags.Bool("annotations", true, "include failure/warning annotations (requires Checks read permission)")
	maxLogBytes := flags.Int64("max-log-bytes", 8<<20, "maximum downloaded bytes per failed job (1..134217728); overflow is partial")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tools github ci failures --run RUN_ID [options]")
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\nFetches all jobs in the selected run attempt; downloads only unsuccessful jobs.\nSupported Go compiler facts are compacted; other evidence retains full available logs.\nPartial results exit 1 and include notes. No waiting, reruns or PR mutations.")
	}
	if err := flags.Parse(args[1:]); err != nil {
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
	if err := compact.validate(); err != nil {
		return fail(err, 2)
	}
	if *maxLogBytes <= 0 {
		return fail(errors.New("--max-log-bytes must be between 1 and 134217728"), 2)
	}
	options := github.CIFailureOptions{RunID: *runID, MaxLogBytes: *maxLogBytes, Annotations: *annotations}
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
	result, err := client.CIFailures(ctx, resolved, options)
	if err != nil {
		return fail(err, 1)
	}
	if *jsonOutput || compact.Enabled {
		if code := encodeCompact(out, stderr, result, compact); code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(out, "%s: %s run=%d attempt=%d head=%s\nfailed jobs: %d\n", result.Status, result.Repo, result.Run.ID, result.Run.Attempt, result.Run.HeadSHA, len(result.Jobs))
		for _, job := range result.Jobs {
			fmt.Fprintf(out, "job %d %s: %s\n  %s\n", job.ID, job.Name, job.Conclusion, job.URL)
			for _, step := range job.Steps {
				if step.Conclusion != "" && step.Conclusion != "success" && step.Conclusion != "skipped" && step.Conclusion != "neutral" {
					fmt.Fprintf(out, "  step %d %s: %s\n", step.Number, step.Name, step.Conclusion)
				}
			}
			for _, annotation := range job.Annotations {
				fmt.Fprintf(out, "  [%s] %s:%d-%d %s\n", annotation.Level, annotation.Path, annotation.StartLine, annotation.EndLine, annotation.Message)
			}
			for _, note := range job.Notes {
				fmt.Fprintln(out, "  note:", note)
			}
		}
		for _, evidence := range result.Evidence {
			if code := encode(out, stderr, evidence); code != 0 {
				return code
			}
		}
		for _, note := range result.Notes {
			fmt.Fprintln(out, "note:", note)
		}
	}
	if !result.Complete {
		return 1
	}
	return 0
}
