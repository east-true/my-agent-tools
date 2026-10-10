package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/filesystem"
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
	flags.BoolVar(&options.Conversation, "conversation", false, "also read paginated ordinary PR comments, preserving exact bodies")
	saveResult := flags.String("save-result", "", "save the complete unabridged JSON once for this task; refuses existing files")
	sourceOptions := reviewSources{}
	sourceOptions.register(flags)
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
	if err := sourceOptions.validate(); err != nil {
		return fail(err, 2)
	}
	if *saveResult != "" {
		if _, err := os.Lstat(*saveResult); err == nil {
			return fail(errors.New("--save-result destination already exists; preserve it and choose a new file before collecting"), 2)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(fmt.Errorf("--save-result destination unavailable: %w", err), 2)
		}
	}
	if sourceOptions.Threads || len(sourceOptions.Paths) > 0 {
		if err := filesystem.ValidatePaths(filesystem.Options{Root: sourceOptions.Root, Paths: sourceOptions.Paths}); err != nil {
			return fail(err, 2)
		}
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
	sources, sourceErr := sourceOptions.read(ctx, &result)
	if sourceErr != nil {
		result.Status, result.Complete = "partial", false
		result.Notes = append(result.Notes, "Local review sources: "+sourceErr.Error())
	}
	if sources != nil && !sources.Complete {
		result.Status, result.Complete = "partial", false
		result.Notes = append(result.Notes, "Selected local source files are incomplete; source_files retains problems or the continuation cursor")
	}
	value := reviewOutput{ReviewResult: result, Sources: sources}
	var saved *savedReviewResult
	if *saveResult != "" && result.Complete {
		saved, err = saveCompleteReviewResult(*saveResult, value)
		if err != nil {
			result.Status, result.Complete = "partial", false
			result.Notes = append(result.Notes, err.Error())
			value.ReviewResult = result
		} else {
			value.Saved = saved
		}
	}
	if *jsonOutput || compact.Enabled {
		if code := encodeCompact(out, stderr, value, compact); code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(out, "%s: PR #%d %s\nhead: %s; verified after collection: %t; review decision: %s\n", result.Status, result.Number, result.URL, result.HeadSHA, result.HeadVerified, result.ReviewDecision)
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
		if result.Conversation != nil {
			for _, comment := range *result.Conversation {
				fmt.Fprintf(out, "conversation %d @%s\n%s\n%s\n", comment.ID, reviewLogin(comment.Author), comment.URL, comment.Body)
			}
		}
		if saved != nil {
			fmt.Fprintf(out, "saved result: %s sha256=%s verified=%t\n", saved.Path, saved.SHA256, saved.Verified)
		}
		if sources != nil {
			for _, file := range sources.Files {
				fmt.Fprintf(out, "source %s sha256=%s\n", file.Path, file.SHA256)
				for _, span := range file.Ranges {
					if span.Text != nil {
						fmt.Fprintln(out, *span.Text)
					}
				}
			}
			for _, problem := range sources.Problems {
				fmt.Fprintf(out, "source problem %s: %s\n", problem.Path, problem.Reason)
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

type savedReviewResult struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Verified bool   `json:"verified"`
}

type reviewOutput struct {
	github.ReviewResult
	Sources *filesystem.Inspection `json:"source_files,omitempty"`
	Saved   *savedReviewResult     `json:"saved_result,omitempty"`
}

func saveCompleteReviewResult(path string, result reviewOutput) (*savedReviewResult, error) {
	if !result.Complete || result.HeadSHA == "" {
		return nil, errors.New("only a complete review collection can be saved")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("save review result (existing files are preserved): %w", err)
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("save review result: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("saved review result is no longer a regular file")
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, actual) {
		return nil, errors.New("saved review result failed read-back verification")
	}
	return &savedReviewResult{Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(actual)), Verified: true}, nil
}

func reviewLogin(author *github.ReviewAuthor) string {
	if author == nil || author.Login == "" {
		return "unknown"
	}
	return author.Login
}
