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
	"strings"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

const help = `tools: repeatable agent workflows

Usage:
  tools github context       Inspect existing labels and issue types
  tools github setup         Fetch labels/types and save project config
  tools github issue create  Create an issue, assign @me, create and link its branch
  tools github issue branch  Create or resume the branch for an existing issue
  tools github pr create     Create a pull request
  tools github branch cleanup Preview or delete branches for finished work

Use '<command> --help' for command-specific options.
`

const githubHelp = `Usage:
  tools github context [--repo OWNER/REPO] [--json]
  tools github setup [--dry-run] [--set-label PREFIX=NAME] [--config FILE]
  tools github issue create --prefix PREFIX --title TITLE --body-file FILE [options]
  tools github issue branch --number NUMBER [options]
  tools github pr create --prefix PREFIX --title TITLE --body-file FILE [options]
  tools github branch cleanup [--apply] [--scope both|local|remote] [--json]

Use --file FILE instead of --title/--body-file for JSON input.
--file - reads JSON from stdin. --dry-run previews without writes.
Creation never commits or pushes.
Use '<command> --help' for command-specific options.
`

const creationHelp = `Create commands fetch and validate existing labels, issue types, and templates; context is optional.
Use --dry-run --json for a plan without writes; reuse the plan unless an error or missing detail needs another lookup.
Pass --repo OWNER/REPO outside a Git checkout.
`

func printHelp(out io.Writer, usage string) {
	fmt.Fprint(out, usage)
	fmt.Fprint(out, "\n", creationHelp)
	fmt.Fprintf(out, "\nAvailable prefixes:\n  %s\n", strings.Join(github.Prefixes, ", "))
}

func printKindHelp(out io.Writer, kind string) {
	var usage strings.Builder
	fmt.Fprintln(&usage, "Usage:")
	fmt.Fprintf(&usage, "  tools github %s create --prefix PREFIX --title TITLE --body-file FILE [options]\n", kind)
	fmt.Fprintf(&usage, "  tools github %s create --file FILE [options]\n", kind)
	if kind == "issue" {
		fmt.Fprintln(&usage, "  tools github issue branch --number NUMBER [options]")
	}
	fmt.Fprintf(&usage, "\nUse 'tools github %s <command> --help' for command-specific options.\n", kind)
	printHelp(out, usage.String())
}

func Run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	return run(ctx, args, in, out, stderr, command.Exec{}, github.NewAPI)
}

func run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer, runner command.Runner, newAPI func(context.Context, command.Runner) (github.API, error)) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printHelp(out, help)
		return 0
	}
	if args[0] != "github" {
		fmt.Fprintf(stderr, "unknown command %q; use tools --help\n", args[0])
		return 2
	}
	args = args[1:]
	action := "context"
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printHelp(out, githubHelp)
		return 0
	}
	kind := args[0]
	if kind == "setup" {
		return runSetup(ctx, args[1:], out, stderr, runner, newAPI)
	}
	if kind == "branch" {
		return runCleanup(ctx, args[1:], out, stderr, runner, newAPI)
	}
	if kind != "context" && kind != "issue" && kind != "pr" {
		fmt.Fprintf(stderr, "unknown github command %q; use tools github --help\n", kind)
		return 2
	}
	args = args[1:]
	if kind != "context" {
		if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
			printKindHelp(out, kind)
			return 0
		}
		action = args[0]
		if action != "create" && !(kind == "issue" && action == "branch") {
			fmt.Fprintf(stderr, "unknown %s action %q\n", kind, action)
			return 2
		}
		args = args[1:]
	}
	flags := flag.NewFlagSet("tools github "+kind, flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub OWNER/REPO (default: current repository)")
	jsonOutput := flags.Bool("json", false, "emit structured JSON")
	configPath := flags.String("config", "", "project .tools.json (default: Git root, then current directory)")
	var file *string
	var dryRun *bool
	var prefix, title, bodyFile *string
	var noBranch, noCheckout *bool
	var number *int
	if kind != "context" {
		dryRun = flags.Bool("dry-run", false, "read GitHub and print the plan without creating anything")
		if action == "create" {
			file = flags.String("file", "", "optional JSON specification file, or - for stdin")
			prefix = flags.String("prefix", "", "choose: "+strings.Join(github.Prefixes, ", "))
			title = flags.String("title", "", "English title; case preserved, only selected prefix added")
			bodyFile = flags.String("body-file", "", "UTF-8 Markdown body file, or - for stdin")
		}
		if kind == "issue" {
			noCheckout = flags.Bool("no-checkout", false, "create the linked remote branch without local checkout")
			if action == "create" {
				noBranch = flags.Bool("no-branch", false, "create only the issue")
			} else {
				number = flags.Int("number", 0, "existing issue number")
			}
		}
	}
	flags.Usage = func() {
		if kind == "context" {
			fmt.Fprintln(stderr, "Usage: tools github context [options]")
		} else {
			fmt.Fprintf(stderr, "Usage: tools github %s %s [options]\n", kind, action)
		}
		flags.PrintDefaults()
		if action == "create" {
			fmt.Fprintf(stderr, "\nExample: tools github %s create --prefix feat --title \"add login\" --body-file body.md\n", kind)
			fmt.Fprintln(stderr, "JSON alternative: {\"prefix\":\"feat\",\"title\":\"add command\",\"summary\":\"기능 추가\"}")
			if kind == "issue" {
				fmt.Fprintln(stderr, "Labels and issue type follow prefix mapping; assignee is always @me. The numbered branch is created and linked automatically.")
			} else {
				fmt.Fprintln(stderr, "Optional fields: issue, base, head, draft, labels, verification [{command,result,details}]. Commit and push first.")
			}
			fmt.Fprintln(stderr, "Use body instead of structured prose for custom Markdown. --file - reads stdin. --dry-run makes no writes.")
			fmt.Fprint(stderr, "\n", creationHelp)
		}
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error, code int) int {
		if *jsonOutput {
			_ = json.NewEncoder(out).Encode(map[string]any{"status": "error", "error": err.Error()})
		} else {
			fmt.Fprintln(stderr, "error:", err)
		}
		return code
	}
	if flags.NArg() != 0 {
		return fail(errors.New("unexpected positional arguments"), 2)
	}
	policy, err := loadPolicy(ctx, runner, *configPath)
	if err != nil {
		return fail(err, 2)
	}
	var spec github.Spec
	if action == "branch" && *number <= 0 {
		return fail(errors.New("--number must be a positive issue number"), 2)
	}
	if action == "create" {
		reader := in
		if *file != "" && (*title != "" || *bodyFile != "") {
			return fail(errors.New("use --file or --title/--body-file, not both"), 2)
		}
		if *file != "" && *file != "-" {
			opened, err := os.Open(*file)
			if err != nil {
				return fail(err, 2)
			}
			defer opened.Close()
			reader = opened
		}
		if *file != "" {
			if err := github.Decode(reader, &spec); err != nil {
				return fail(err, 2)
			}
		} else {
			if *title == "" || *bodyFile == "" {
				return fail(errors.New("provide --prefix, --title and --body-file (or --file JSON)"), 2)
			}
			if *bodyFile != "-" {
				opened, err := os.Open(*bodyFile)
				if err != nil {
					return fail(err, 2)
				}
				defer opened.Close()
				reader = opened
			}
			body, err := io.ReadAll(reader)
			if err != nil {
				return fail(err, 2)
			}
			spec.Title, spec.Body = *title, string(body)
		}
		if *prefix != "" {
			if spec.Prefix != "" && spec.Prefix != *prefix {
				return fail(errors.New("--prefix conflicts with JSON prefix"), 2)
			}
			spec.Prefix = *prefix
		}
		if err := spec.ApplyPrefix(); err != nil {
			return fail(err, 2)
		}
		if _, err := spec.Validate(kind, policy.BodyLanguage); err != nil {
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
	if kind == "context" {
		catalog, err := client.Context(ctx, resolved)
		if err != nil {
			return fail(err, 1)
		}
		if *jsonOutput {
			return encode(out, stderr, map[string]any{"status": "ok", "catalog": catalog, "policy": policy})
		}
		fmt.Fprintf(out, "repo: %s\ndefault branch: %s\nlabels:", resolved, catalog.Repository.DefaultBranch)
		for _, item := range catalog.Labels {
			fmt.Fprintf(out, " %s;", item.Name)
		}
		fmt.Fprint(out, "\nissue types:")
		for _, item := range catalog.IssueTypes {
			if item.Enabled == nil || *item.Enabled {
				fmt.Fprintf(out, " %s;", item.Name)
			}
		}
		fmt.Fprintln(out)
		for _, note := range catalog.Notes {
			fmt.Fprintln(out, "note:", note)
		}
		return 0
	}
	if action == "branch" {
		plan, result, err := client.ExistingIssuePlan(ctx, resolved, *number)
		if err != nil {
			return fail(err, 1)
		}
		if err := client.PrepareBranch(ctx, &plan, !*noCheckout); err != nil {
			return fail(err, 1)
		}
		if *dryRun {
			return encode(out, stderr, map[string]any{"status": "planned", "plan": plan, "issue_url": result.URL})
		}
		result.Notes = plan.Notes
		err = client.CompleteBranch(ctx, plan, &result)
		return finish(out, stderr, *jsonOutput, result, err)
	}
	plan, err := client.Prepare(ctx, resolved, kind, spec, policy)
	if err != nil {
		return fail(err, 1)
	}
	if kind == "issue" && !*noBranch {
		if err := client.PrepareBranch(ctx, &plan, !*noCheckout); err != nil {
			return fail(err, 1)
		}
	}
	if *dryRun {
		return encode(out, stderr, map[string]any{"status": "planned", "plan": plan})
	}
	result, err := client.Create(ctx, plan)
	if err != nil && result.Number == 0 {
		return fail(err, 1)
	}
	if err == nil && kind == "issue" {
		err = client.CompleteBranch(ctx, plan, &result)
	}
	return finish(out, stderr, *jsonOutput, result, err)
}

func finish(out, stderr io.Writer, jsonOutput bool, result github.Result, err error) int {
	if err != nil {
		result.Status = "partial"
		result.Error = err.Error()
	}
	if jsonOutput {
		if err != nil {
			result.Error = err.Error()
		}
		code := encode(out, stderr, result)
		if err != nil {
			return 1
		}
		return code
	}
	fmt.Fprintf(out, "%s: %s\n", result.Status, result.URL)
	if len(result.Selection.Labels) > 0 {
		fmt.Fprintln(out, "labels:", strings.Join(result.Selection.Labels, ", "))
	}
	if result.Selection.IssueType != "" {
		fmt.Fprintln(out, "issue type:", result.Selection.IssueType)
	}
	if result.BranchInfo != nil {
		fmt.Fprintf(out, "branch: %s (linked=%t, checked_out=%t)\n", result.BranchInfo.Name, result.BranchInfo.Linked, result.BranchInfo.CheckedOut)
	} else if result.Branch != "" {
		fmt.Fprintln(out, "suggested branch:", result.Branch)
	}
	for _, note := range result.Notes {
		fmt.Fprintln(out, "note:", note)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func encode(out, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(stderr, "output:", err)
		return 1
	}
	return 0
}

func loadPolicy(ctx context.Context, runner command.Runner, path string) (github.Policy, error) {
	policy := github.DefaultPolicy()
	explicit := path != ""
	path = resolvePolicyPath(ctx, runner, path)
	file, err := os.Open(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return policy, nil
		}
		return policy, err
	}
	defer file.Close()
	var config github.Config
	if err := github.Decode(file, &config); err != nil {
		return policy, fmt.Errorf("config %s: %w", path, err)
	}
	return policy, policy.Merge(config.GitHub)
}

func resolvePolicyPath(ctx context.Context, runner command.Runner, path string) string {
	if path == "" {
		root, err := runner.Run(ctx, nil, "git", "rev-parse", "--show-toplevel")
		if err == nil {
			path = filepath.Join(strings.TrimSpace(string(root)), ".tools.json")
		} else {
			path = ".tools.json"
		}
	}
	return path
}
