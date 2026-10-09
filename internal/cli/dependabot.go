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

func runDependabot(ctx context.Context, args []string, out, stderr io.Writer, runner command.Runner, newAPI func(context.Context, command.Runner) (github.API, error)) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, "Usage:\n  tools github dependabot list [options]\n  tools github dependabot view --number NUMBER [options]\n\nRead dependency security alerts. List defaults to open alerts and fetches all pages.\nUse '<command> --help' for options.")
		return 0
	}
	action := args[0]
	if action != "list" && action != "view" {
		fmt.Fprintf(stderr, "unknown dependabot action %q\n", action)
		return 2
	}
	flags := flag.NewFlagSet("tools github dependabot "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "GitHub OWNER/REPO (default: current repository)")
	jsonOutput := flags.Bool("json", false, "emit structured JSON")
	options := github.DependabotOptions{}
	var number int
	if action == "list" {
		flags.StringVar(&options.State, "state", "open", "open, fixed, dismissed, auto_dismissed (comma-separated), or all")
		flags.StringVar(&options.Severity, "severity", "", "low, medium, high, critical (comma-separated)")
		flags.StringVar(&options.Ecosystem, "ecosystem", "", "package ecosystems, comma-separated (e.g. go,npm)")
		flags.StringVar(&options.Package, "package", "", "package names, comma-separated")
	} else {
		flags.IntVar(&number, "number", 0, "Dependabot alert number (required)")
	}
	flags.Usage = func() {
		fmt.Fprintf(stderr, "Usage: tools github dependabot %s [options]\n", action)
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\nRead-only. List fetches all pages; view includes the advisory description and references.\nAuthentication uses GH_TOKEN, GITHUB_TOKEN, or optional gh auth token.\nFine-grained tokens need Dependabot alerts read permission.")
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
	if action == "view" && number <= 0 {
		return fail(errors.New("--number must be a positive alert number"), 2)
	}
	if action == "list" {
		if err := options.Normalize(); err != nil {
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
	if action == "view" {
		alert, err := client.ViewDependabot(ctx, resolved, number)
		if err != nil {
			return fail(err, 1)
		}
		if *jsonOutput {
			return encode(out, stderr, map[string]any{"status": "ok", "repo": resolved, "alert": alert})
		}
		printDependabotAlert(out, alert)
		if alert.Description != "" {
			fmt.Fprintln(out, "\n"+alert.Description)
		}
		for _, ref := range alert.References {
			fmt.Fprintln(out, "reference:", ref)
		}
		if alert.DismissedReason != "" {
			fmt.Fprintln(out, "dismissed reason:", alert.DismissedReason)
		}
		if alert.DismissedComment != "" {
			fmt.Fprintln(out, "dismissed comment:", alert.DismissedComment)
		}
		return 0
	}
	alerts, err := client.ListDependabot(ctx, resolved, options)
	if err != nil {
		return fail(err, 1)
	}
	if *jsonOutput {
		return encode(out, stderr, map[string]any{"status": "ok", "repo": resolved, "filters": options, "count": len(alerts), "alerts": alerts})
	}
	fmt.Fprintf(out, "repo: %s\nDependabot alerts: %d (state=%s)\n", resolved, len(alerts), options.State)
	for _, alert := range alerts {
		printDependabotAlert(out, alert)
	}
	return 0
}

func printDependabotAlert(out io.Writer, alert github.DependabotAlert) {
	patch := "not available"
	if alert.PatchedVersion != nil {
		patch = *alert.PatchedVersion
	}
	fmt.Fprintf(out, "#%d [%s] %s %s (%s)\n  %s\n  manifest: %s; scope: %s\n  vulnerable: %s; first patched: %s\n  %s %s\n  %s\n",
		alert.Number, alert.Severity, alert.State, alert.Package, alert.Ecosystem,
		alert.Summary, alert.ManifestPath, alert.Scope, alert.VulnerableRange, patch, alert.GHSAID, alert.CVEID, alert.URL)
}
