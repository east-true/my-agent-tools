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

func runPRReviews(ctx context.Context, args []string, out, stderr io.Writer, runner command.Runner, newAPI func(context.Context, command.Runner) (github.API, error)) int {
	flags := flag.NewFlagSet("tools github pr reviews", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub OWNER/REPO (default: current repository)")
	jsonOutput := flags.Bool("json", false, "emit review bodies, thread locations and full conversations")
	options := github.ReviewOptions{}
	compact := compactFlags{}
	compact.register(flags, false)
	flags.IntVar(&options.Number, "number", 0, "pull request number (required)")
	flags.BoolVar(&options.All, "all", false, "include resolved threads (default: unresolved, including outdated threads)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tools github pr reviews --number NUMBER [options]")
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\nReads submitted review history and paginates threads and their replies.\nPreserves bodies verbatim, authors, current/original locations and outdated flags.\nReview history includes dismissed reviews; review_decision is the current aggregate.\nPartial collection exits 1 with notes. This command is read-only.")
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
	if err := compact.validate(); err != nil {
		return fail(err, 2)
	}
	if err := options.Validate(); err != nil {
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
	result, err := client.PullRequestReviews(ctx, resolved, options)
	if err != nil {
		return fail(err, 1)
	}
	if *jsonOutput || compact.Enabled {
		if code := encodeCompact(out, stderr, result, compact); code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(out, "%s: PR #%d %s\nhead: %s; review decision: %s\n", result.Status, result.Number, result.URL, result.HeadSHA, result.ReviewDecision)
		for _, review := range result.Reviews {
			fmt.Fprintf(out, "review %d [%s] @%s commit=%s\n%s\n%s\n", review.ID, review.State, reviewLogin(review.Author), review.CommitID, review.URL, review.Body)
		}
		for _, thread := range result.Threads {
			line, original := "file", "unknown"
			if thread.Line != nil {
				line = fmt.Sprint(*thread.Line)
			}
			if thread.OriginalLine != nil {
				original = fmt.Sprint(*thread.OriginalLine)
			}
			fmt.Fprintf(out, "thread %s %s:%s side=%s original=%s resolved=%t outdated=%t\n", thread.ID, thread.Path, line, thread.DiffSide, original, thread.Resolved, thread.Outdated)
			for _, comment := range thread.Comments {
				fmt.Fprintf(out, "  @%s %s\n%s\n%s\n", reviewLogin(comment.Author), comment.CreatedAt, comment.URL, comment.Body)
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

func reviewLogin(author *github.ReviewAuthor) string {
	if author == nil || author.Login == "" {
		return "unknown"
	}
	return author.Login
}
