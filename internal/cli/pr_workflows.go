package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/filesystem"
	"github.com/east-true/my-agent-tools/internal/github"
)

func runPRWorkflow(ctx context.Context, action string, args []string, in io.Reader, out, stderr io.Writer, runner command.Runner, newAPI func(context.Context, command.Runner) (github.API, error)) int {
	flags := flag.NewFlagSet("tools github pr "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub OWNER/REPO (default: current origin)")
	flags.Bool("json", false, "emit one structured result (workflow commands always return JSON)")
	evidenceReader := evidenceReadFlags{}
	if action != "delta" {
		evidenceReader.register(flags)
	}
	compact := compactFlags{}
	compact.register(flags, action != "delta")
	number := 0
	if action != "submit" {
		flags.IntVar(&number, "number", 0, "pull request number (required)")
	}
	inspect := github.InspectOptions{}
	if action != "delta" {
		flags.DurationVar(&inspect.Timeout, "timeout", 10*time.Minute, "total inspection/merge waiting limit")
		flags.DurationVar(&inspect.Interval, "interval", 10*time.Second, "polling interval")
	}
	if action == "inspect" || action == "submit" {
		flags.StringVar(&inspect.Sections, "sections", "all", "select checks,reviews,failures; PR metadata is always included")
		flags.BoolVar(&inspect.Conversation, "conversation", false, "include paginated ordinary PR comments in the reviews section")
		flags.BoolVar(&inspect.Wait, "wait", action == "submit", "wait internally for checks to finish")
		flags.BoolVar(&inspect.Annotations, "annotations", true, "include failed CI annotations")
		flags.Int64Var(&inspect.MaxLogBytes, "max-log-bytes", 8<<20, "maximum bytes downloaded per failed job")
	}
	sourceOptions := reviewSources{}
	if action == "inspect" {
		sourceOptions.register(flags)
	}
	statePath, full := "", false
	if action == "inspect" {
		flags.StringVar(&statePath, "state-file", "", "save complete observations and return only changes on subsequent calls")
		flags.BoolVar(&full, "full", false, "return the full current snapshot even with an existing state file")
	}
	delta := github.DeltaOptions{}
	if action == "delta" {
		flags.StringVar(&delta.Since, "since", "", "full baseline commit SHA (required)")
		flags.BoolVar(&delta.IncludePatch, "include-patch", false, "include file patches; default is metadata only")
	}
	remote := "origin"
	if action == "submit" {
		flags.StringVar(&remote, "remote", "origin", "Git remote; must match the repository for push")
	}
	spec := github.Spec{}
	file, bodyFile, configPath := "", "", ""
	dryRun := false
	if action == "submit" {
		flags.StringVar(&spec.Prefix, "prefix", "", "PR title prefix")
		flags.StringVar(&spec.Title, "title", "", "English PR title")
		flags.StringVar(&bodyFile, "body-file", "", "Markdown PR body file, or - for stdin")
		flags.StringVar(&file, "file", "", "JSON PR specification, or - for stdin")
		flags.StringVar(&configPath, "config", "", "project .tools.json")
		flags.BoolVar(&dryRun, "dry-run", false, "validate local submission without pushing or creating a PR")
	}
	flags.Usage = func() {
		fmt.Fprintf(stderr, "Usage: tools github pr %s [options]\n", action)
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\ninspect: read metadata, reviews, checks, run IDs and exact-attempt failure evidence.\nsubmit: push existing commits, reuse/create a PR, then inspect checks; never commits or merges.\ndelta: compare baseline to current head; file patches are opt-in.\nLong compact evidence is preserved by file path and SHA-256; short outputs are unchanged.")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error, code int) int {
		_ = encode(out, stderr, map[string]any{"status": "error", "error": err.Error()})
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
	if action != "submit" && number <= 0 {
		return fail(errors.New("--number must be a positive PR number"), 2)
	}
	if action != "delta" && (inspect.Timeout <= 0 || inspect.Interval <= 0) {
		return fail(errors.New("--timeout and --interval must be positive"), 2)
	}
	if action == "inspect" || action == "submit" {
		if inspect.MaxLogBytes <= 0 || inspect.MaxLogBytes > 128<<20 {
			return fail(errors.New("--max-log-bytes must be between 1 and 134217728"), 2)
		}
		inspect.Number = number
		if action == "submit" {
			inspect.Number = 1
		}
		if err := inspect.Normalize(); err != nil {
			return fail(err, 2)
		}
	}
	if action == "inspect" {
		if err := sourceOptions.validate(); err != nil {
			return fail(err, 2)
		}
		if (sourceOptions.Threads || len(sourceOptions.Paths) > 0) && !includesReviews(inspect.Sections) {
			return fail(errors.New("review source selection requires the reviews section"), 2)
		}
		if sourceOptions.Threads || len(sourceOptions.Paths) > 0 {
			if err := filesystem.ValidatePaths(filesystem.Options{Root: sourceOptions.Root, Paths: sourceOptions.Paths}); err != nil {
				return fail(err, 2)
			}
		}
	}
	if action == "delta" {
		delta.Number = number
		if err := delta.Validate(); err != nil {
			return fail(err, 2)
		}
	}
	policy := github.DefaultPolicy()
	if action == "submit" {
		var err error
		policy, err = loadPolicy(ctx, runner, configPath)
		if err != nil {
			return fail(err, 2)
		}
		if err := readSubmitSpec(file, bodyFile, in, &spec); err != nil {
			return fail(err, 2)
		}
		if err := spec.ApplyPrefix(); err != nil {
			return fail(err, 2)
		}
		if _, err := spec.Validate("pr", policy.BodyLanguage); err != nil {
			return fail(err, 2)
		}
	}
	client := github.Client{Runner: runner}
	resolved, err := client.ResolveRepo(ctx, *repo)
	if err != nil {
		return fail(err, 1)
	}
	var before *inspectionState
	if statePath != "" {
		statePath, err = filepath.Abs(statePath)
		if err != nil {
			return fail(err, 2)
		}
		unlock, err := lockInspectionState(statePath)
		if err != nil {
			return fail(err, 1)
		}
		defer unlock()
		before, err = readInspectionState(statePath, resolved, number, inspect.Sections)
		if err != nil {
			return fail(err, 2)
		}
		if before != nil && before.Conversation != inspect.Conversation {
			return fail(errors.New("--state-file conversation scope differs; use a separate state file"), 2)
		}
		reuseInspectionEvidence(before, &inspect)
	}
	if action != "submit" || !dryRun {
		client.API, err = newAPI(ctx, runner)
		if err != nil {
			return fail(err, 1)
		}
	}
	switch action {
	case "delta":
		result, err := client.PullRequestDelta(ctx, resolved, delta)
		if err != nil {
			return fail(err, 1)
		}
		if code := encodeCompact(out, stderr, result, compact); code != 0 {
			return code
		}
		if !result.Complete {
			return 1
		}
	case "inspect":
		inspect.Number = number
		result, err := client.InspectPR(ctx, resolved, inspect)
		if err != nil {
			return fail(err, 1)
		}
		sources, sourceErr := sourceOptions.read(ctx, result.Reviews)
		if sourceErr != nil {
			result.Status, result.Complete = "partial", false
			result.Notes = append(result.Notes, "Local review sources: "+sourceErr.Error())
		}
		if sources != nil && !sources.Complete {
			result.Complete = false
		}
		var value any = result
		var after inspectionState
		if statePath != "" {
			value, after = inspectionDelta(result, before, full, statePath)
			after.Annotations, after.MaxLogBytes = inspect.Annotations, inspect.MaxLogBytes
			after.Conversation = inspect.Conversation
		}
		value, err = attachReviewSources(value, sources)
		if err != nil {
			return fail(err, 1)
		}
		if code := encodeCompact(out, stderr, value, compact); code != 0 {
			return code
		}
		if !result.Complete {
			return 1
		}
		if statePath != "" {
			if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
				fmt.Fprintln(stderr, "save inspection state:", err)
				return 1
			}
			data, _ := json.Marshal(after)
			if err := atomicJSONFile(statePath, data); err != nil {
				fmt.Fprintln(stderr, "save inspection state:", err)
				return 1
			}
		}
	case "submit":
		result, err := client.SubmitPR(ctx, resolved, github.SubmitOptions{Spec: spec, Policy: policy, Remote: remote, Wait: inspect.Wait, DryRun: dryRun, Inspect: inspect})
		if code := encodeCompact(out, stderr, result, compact); code != 0 {
			return code
		}
		if err != nil || (result.Status != "submitted" && result.Status != "planned") {
			return 1
		}

	}
	return 0
}

func readSubmitSpec(file, bodyFile string, in io.Reader, spec *github.Spec) error {
	if file != "" {
		if spec.Title != "" || bodyFile != "" {
			return errors.New("use --file or --title/--body-file, not both")
		}
		prefix := spec.Prefix
		reader := in
		if file != "-" {
			opened, err := os.Open(file)
			if err != nil {
				return err
			}
			defer opened.Close()
			reader = opened
		}
		if err := github.Decode(reader, spec); err != nil {
			return err
		}
		if prefix != "" {
			if spec.Prefix != "" && spec.Prefix != prefix {
				return errors.New("--prefix conflicts with JSON prefix")
			}
			spec.Prefix = prefix
		}
		return nil
	}
	if spec.Title == "" || bodyFile == "" {
		return errors.New("--title and --body-file (or --file) are required")
	}
	reader := in
	if bodyFile != "-" {
		opened, err := os.Open(bodyFile)
		if err != nil {
			return err
		}
		defer opened.Close()
		reader = opened
	}
	body, err := io.ReadAll(reader)
	spec.Body = string(body)
	return err
}
