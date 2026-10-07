# my-agent-tools

**Repeatable workflows in one command, for coding agents and developers.**

[한국어](docs/README.ko.md) · [Workflows](#available-workflows) · [Benchmarks](docs/benchmarks/README.md) · [Contributing](CONTRIBUTING.md)

`tools` is a cross-platform CLI for automating repeatable workflows. It combines routine steps into commands that agents and developers can reuse across projects, with project-specific preferences kept in configuration.

Workflows follow `tools <group> <command>`. GitHub is the first available group; additional workflows will be added as separate command groups.

## Available workflows

| Group | What it automates | Documentation |
|---|---|---|
| `github` | Repository setup, issues, linked branches, PRs and reviews, CI diagnostics and reruns, Dependabot alerts, and finished-branch cleanup | [GitHub commands](docs/github/README.md) |

## Token usage measurements

The table below retains the historical `gpt-6.1-sol` measurements. See the [Astra and Luna comparison](docs/benchmarks/github/models.md) for nine tasks, 54 fresh sessions per model, and token means alongside correctness rates.

Current measurements cover the GitHub group. Branch cleanup used 56.1% fewer total agent tokens than direct `gh + git` in a controlled 14-branch experiment. Issue planning showed little difference.

| Command | Direct `gh` / `gh + git` | `tools` | Total token change |
|---|---:|---:|---:|
| [`github context`](docs/benchmarks/github/context.md) | 30,221 | 29,894 | −1.1% |
| [`github setup`](docs/benchmarks/github/setup.md) | 53,970 | 30,473 | −43.5% |
| [`github dependabot list / view`](docs/benchmarks/github/dependabot.md) | 31,209 | 30,715 | −1.6% |
| [`github issue create --dry-run`](docs/benchmarks/github/issue-create.md) | 46,630 | 46,279 | −0.75% |
| [`github issue create`](docs/benchmarks/github/issue-create.md#실제-실험-이슈브랜치-생성) | 116,141 | 34,768 | −70.1% |
| [`github issue branch`](docs/benchmarks/github/issue-branch.md) | 113,659 | 29,904 | −73.7% |
| [`github pr create`](docs/benchmarks/github/pr-create.md) | 102,598 | 45,076 | −56.1% |
| [`github branch cleanup`](docs/benchmarks/github/branch-cleanup.md#미리보기) | 53,351 | 30,987 | −41.9% |
| [`github branch cleanup --apply`](docs/benchmarks/github/branch-cleanup.md) | 70,552 | 31,006 | −56.1% |

Means for the whole agent task, including cached input and output: `gpt-6.1-sol`, high reasoning, three fresh sessions per method. The CLI itself makes no model calls. The seven newly measured tasks used a local fixture API and real disposable Git repositories; all 42 compared runs passed answer and state checks. Issue creation includes linked-branch checkout; issue branching includes a repeated resume, and Dependabot combines list and view. Earlier dry-run and cleanup-apply results retain their own conditions. Cache was uncontrolled and corrected direct baselines ran later, so total token changes do not establish monetary savings. [Raw evidence, exclusions, and reproduction](docs/benchmarks/README.md).

## Install

From a source checkout, build with **Go 1.26+**:

```sh
go build -o tools ./cmd/tools
```

On Windows, use `go build -o tools.exe ./cmd/tools`. Add the binary directory to your PATH. The binary runs on Linux, macOS, and Windows without a Go runtime.

## Quick start

List available workflows and commands:

```sh
tools --help
```

### GitHub

Authenticate with `GH_TOKEN` or `GITHUB_TOKEN`, or reuse an existing `gh auth login`. With an environment token, `gh` is optional. This group currently supports `github.com`; local branch operations require Git.

Run inside your target repository. Save your issue and PR descriptions as UTF-8 Markdown files, `issue.md` and `pr.md`.

```sh
# Import existing labels/types and allow bodies in any language.
tools github setup --body-language any

# Create an issue, assign yourself, and check out its linked branch.
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json

# Implement and verify your changes, then commit and push with Git.
tools github pr create --prefix fix --title "handle duplicate requests" --body-file pr.md --json

# Collect submitted reviews and unresolved conversations with code locations.
tools github pr reviews --number 123 --json

# Wait for checks, merge when ready, or return failure evidence in one call.
tools github pr merge --number 123 --json
```

Issue creation retrieves the metadata it needs; `context` is optional. Add `--dry-run --json` to preview a creation plan. Outside a Git checkout, pass `--repo OWNER/REPO`. Bodies default to Korean unless configured otherwise; [setup](docs/github/setup.md) saves your language and label preferences.

Retry failed CI jobs and wait for the new attempt, with failure evidence if it fails again:

```sh
tools github ci rerun --run 123456789 --json
```

Use `--all` for the entire workflow, `--wait=false` to return after request acceptance, or `--dry-run` to preview. Reruns use the original commit; after pushing a fix, inspect the new commit's workflow run. [CI rerun options and recovery](docs/github/ci/rerun.md).

Read dependency security alerts and inspect an alert's advisory and patch:

```sh
tools github dependabot list --severity high,critical --json
tools github dependabot view --number 7 --json
```

Listing defaults to open alerts and retrieves every page. Fine-grained tokens need Dependabot alerts read permission. [List options and output](docs/github/dependabot/list.md) · [Alert details](docs/github/dependabot/view.md).

After the PR or issue closes, switch away from the finished branch and preview cleanup:

```sh
tools github branch cleanup --json
# Inspect the candidates, then delete them.
tools github branch cleanup --apply --json
```

Cleanup preserves protected branches, branches checked out in worktrees, open PR work, and unpublished local commits. Closed, unmerged remote work is also eligible; inspect the preview. [Cleanup rules and options](docs/github/branch/cleanup.md).

## Documentation

- [GitHub command reference (Korean)](docs/github/README.md) — commands, options, configuration, and recovery.
- [GitHub agent usage (Korean)](docs/agent-usage.md) — a short workflow for coding agents.
- [Input and policy examples](examples/github) — issue/PR JSON, Markdown, and `.tools.json`.
- [Benchmarks](docs/benchmarks/README.md) — command measurements, raw data, and experimental workflows.
- [Development and leak checks](docs/development.md) · [CI report experiment](docs/ci-reports.md).

Use `tools --help` to discover commands and a command's `--help` for its options.

## Contributing

New workflow groups, bug reports, and contributions from other projects are welcome. See [Contributing](CONTRIBUTING.md), [Security](SECURITY.md), and [repository policies](docs/repository.md).

Licensed under the [MIT License](LICENSE).
