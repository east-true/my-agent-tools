package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

func runCleanup(ctx context.Context, args []string, out, stderr io.Writer, runner command.Runner, newAPI func(context.Context, command.Runner) (github.API, error)) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, "Usage: tools github branch cleanup [options]\n\nPreview branches whose latest PR is merged/closed or whose issue is closed.\nUse 'tools github branch cleanup --help' for options.")
		return 0
	}
	if args[0] != "cleanup" {
		fmt.Fprintf(stderr, "unknown branch action %q\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("tools github branch cleanup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub OWNER/REPO (must match the selected Git remote)")
	remote := flags.String("remote", "origin", "Git remote to inspect and clean")
	scope := flags.String("scope", "both", "both, local, or remote")
	protect := flags.String("protect", "", "additional protected branch names/globs, comma-separated")
	branch := flags.String("branch", "", "limit planning and cleanup to this exact remote branch and local aliases")
	apply := flags.Bool("apply", false, "remove eligible worktrees and branches; default is read-only preview")
	dryRun := flags.Bool("dry-run", false, "explicit read-only preview (cannot combine with --apply)")
	jsonOutput := flags.Bool("json", false, "emit structured plan/results")
	includeSkipped := flags.Bool("include-skipped", false, "include unchanged branches and reasons (default: candidates/actions and counts)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tools github branch cleanup [options]")
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\nDefault: preview only. --apply rechecks GitHub states and expected commit SHAs.\nClean linked worktrees for finished branches are removed with local cleanup.\nMain/current, locked or dirty worktrees, protected branches and open PR work are retained.\nLocal commits not verified as published are retained. --apply also deletes\nclosed but unmerged remote work; inspect the preview before applying.")
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
	if *apply && *dryRun {
		return fail(errors.New("--apply and --dry-run cannot be combined"), 2)
	}
	if *scope != "both" && *scope != "local" && *scope != "remote" {
		return fail(errors.New("--scope must be both, local, or remote"), 2)
	}
	client := github.Client{Runner: runner}
	resolved, err := client.ResolveRemoteRepo(ctx, *repo, *remote)
	if err != nil {
		return fail(err, 1)
	}
	client.API, err = newAPI(ctx, runner)
	if err != nil {
		return fail(err, 1)
	}
	options := github.CleanupOptions{Remote: *remote, Scope: *scope, Branch: *branch}
	if *protect != "" {
		for _, pattern := range strings.Split(*protect, ",") {
			options.Protect = append(options.Protect, strings.TrimSpace(pattern))
		}
	}
	plan, err := client.PlanCleanup(ctx, resolved, options)
	if err != nil {
		return fail(err, 1)
	}
	if !*apply {
		visible := plan
		visible.Targets = []github.CleanupTarget{}
		eligible, kept := 0, 0
		for _, target := range plan.Targets {
			if target.Eligible {
				eligible++
			} else {
				kept++
			}
			if target.Eligible || *includeSkipped {
				visible.Targets = append(visible.Targets, target)
			}
		}
		if *jsonOutput {
			return encode(out, stderr, map[string]any{"status": "planned", "plan": visible, "summary": map[string]int{"eligible": eligible, "kept": kept}})
		}
		fmt.Fprintf(out, "preview: %s (remote=%s, scope=%s)\n", resolved, *remote, *scope)
		for _, target := range visible.Targets {
			if target.Eligible {
				fmt.Fprintf(out, "delete %s %s: %s\n", target.Scope, target.Name, strings.Join(target.Reasons, "; "))
			} else {
				fmt.Fprintf(out, "keep %s %s: %s\n", target.Scope, target.Name, target.Skip)
			}
			if target.WorktreePath != "" {
				fmt.Fprintln(out, "  worktree:", target.WorktreePath)
			}
		}
		fmt.Fprintf(out, "eligible: %d; kept: %d (use --include-skipped for keep reasons)\n", eligible, kept)
		fmt.Fprintln(out, "Use --apply to remove eligible worktrees and branches.")
		return 0
	}
	result, err := client.ApplyCleanup(ctx, plan)
	if err != nil && len(result.Actions) == 0 {
		return fail(err, 1)
	}
	if *jsonOutput {
		visible := result
		visible.Actions = []github.CleanupAction{}
		counts := map[string]int{"deleted": 0, "skipped": 0, "error": 0}
		for _, action := range result.Actions {
			counts[action.Status]++
			if action.Eligible || action.Status != "skipped" || *includeSkipped {
				visible.Actions = append(visible.Actions, action)
			}
		}
		code := encode(out, stderr, map[string]any{"status": visible.Status, "repo": visible.Repo, "actions": visible.Actions, "summary": counts})
		if err != nil {
			return 1
		}
		return code
	}
	for _, action := range result.Actions {
		if !action.Eligible && action.Status == "skipped" && !*includeSkipped {
			continue
		}
		fmt.Fprintf(out, "%s %s %s", action.Status, action.Scope, action.Name)
		if action.WorktreePath != "" {
			fmt.Fprintf(out, " (%s)", action.WorktreePath)
		}
		if action.Error != "" {
			fmt.Fprint(out, ": ", action.Error)
		} else if action.Status == "skipped" {
			fmt.Fprint(out, ": ", action.Skip)
		}
		if action.Warning != "" {
			fmt.Fprint(out, "; ", action.Warning)
		}
		fmt.Fprintln(out)
	}
	if err != nil {
		return 1
	}
	return 0
}
