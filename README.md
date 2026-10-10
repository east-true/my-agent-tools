# my-agent-tools

**Repeatable workflows in one command, for coding agents and developers.**

[한국어](docs/README.ko.md) · [Workflows](#available-workflows) · [Benchmarks](docs/benchmarks/README.md) · [Contributing](CONTRIBUTING.md)

`tools` is a cross-platform CLI for automating repeatable workflows. It combines routine steps into commands that agents and developers can reuse across projects, with project-specific preferences kept in configuration.

Workflows follow `tools <group> <command>`, with separate GitHub and filesystem groups.

## Available workflows

| Group | What it automates | Documentation |
|---|---|---|
| `github` | Repository setup, issues, linked branches, PRs and reviews, CI diagnostics and reruns, Dependabot alerts, and finished-branch cleanup | [GitHub commands](docs/github/README.md) |
| `fs` | Raw files/Markdown sections, deltas, verified batch edits, and JUnit report evidence | [Filesystem commands](docs/fs/README.md) |

## Token usage measurements

Latest results per command, with three trials per method. The table shows **mean uncached input + output** and the change in total tokens including cached input. Dates use Korea Standard Time; each command page provides ranges and answer/state checks.

Inherited defaults: 2026-10-08: `gpt-6.1-sol` / `high`; 2026-10-10: `gpt-6.1-sol` / `low`; 2026-10-11: `gpt-6.1-sol` / `high`. Batches with different settings are not pooled.

| Command | Date | Direct mean | tools mean | Change | Total change (incl. cache) |
|---|---|---:|---:|---:|---:|
| [`github context`](docs/benchmarks/github/context.md) | 2026-10-08 | 15,573 | 15,324 | −1.6% | −0.8% |
| [`github setup`](docs/benchmarks/github/setup.md) | 2026-10-08 | 17,343 | 15,695 | −9.5% | −36.2% |
| [`github issue create`](docs/benchmarks/github/issue/create.md) | 2026-10-08 | 18,583 | 20,244 | +8.9% | −62.4% |
| [`github issue branch`](docs/benchmarks/github/issue/branch.md) | 2026-10-08 | 15,314 | 15,626 | +2.0% | −40.8% |
| [`github pr create`](docs/benchmarks/github/pr/create.md) | 2026-10-11 | 18,491 | 15,713 | −15.0% | −59.0% |
| [`github pr reviews`](docs/benchmarks/github/pr/reviews.md) | 2026-10-11 | 13,978 | 18,389 | +31.6% | +0.6% |
| [`github pr inspect`](docs/benchmarks/github/pr/inspect.md) | 2026-10-10 | 19,458 | 17,753 | −8.8% | −14.3% |
| [`github pr delta`](docs/benchmarks/github/pr/delta.md) | 2026-10-08 | 16,851 | 15,314 | −9.1% | −51.7% |
| [`github pr submit`](docs/benchmarks/github/pr/submit.md) | 2026-10-08 | 19,974 | 15,777 | −21.0% | −66.0% |
| [`github pr merge`](docs/benchmarks/github/pr/merge.md) | 2026-10-08 | 20,302 | 11,468 | −43.5% | −76.3% |
| [`github ci failures`](docs/benchmarks/github/ci/failures.md) | 2026-10-08 | 21,616 | 16,019 | −25.9% | −50.3% |
| [`github ci rerun`](docs/benchmarks/github/ci/rerun.md) | 2026-10-08 | 18,078 | 15,673 | −13.3% | −61.7% |
| [`github dependabot list`](docs/benchmarks/github/dependabot/list.md) | 2026-10-08 | 16,036 | 15,503 | −3.3% | −2.2% |
| [`github dependabot view`](docs/benchmarks/github/dependabot/view.md) | 2026-10-08 | 11,243 | 15,175 | +35.0% | −0.7% |
| [`github branch cleanup --apply`](docs/benchmarks/github/branch/cleanup.md) | 2026-10-08 | 30,151 | 21,365 | −29.1% | −54.9% |

These are small-sample results for a fixed synthetic workload; cache hit differences remain. They do not establish simultaneous efficiency and token improvements for independent one-use or return-after-other-work scenarios, general savings, or monetary savings. [Shared protocol, limits, evidence, and reproduction](docs/benchmarks/github/README.md).

### Filesystem measurements

Filesystem defaults by cohort: 2026-10-10: `gpt-6.1-sol` / `low`; 2026-10-11: `gpt-6.1-sol` / `high`. Most rows use three pairs; JUnit normal reading uses two. Means use uncached input + output; total changes include cached input.

| Command | Direct mean | tools mean | Uncached change | Total change (incl. cache) | Answer + state | Joint assessment |
|---|---:|---:|---:|---:|---:|---|
| [`fs inspect: Markdown`](docs/benchmarks/fs/inspect.md) | 21,294 | 16,324 | −23.3% | −42.0% | 6/6 | AND met in sample |
| [`fs inspect: raw file`](docs/benchmarks/fs/inspect.md) | 16,459 | 15,601 | −5.2% | −2.3% | 6/6 | AND met in sample |
| [`fs delta`](docs/benchmarks/fs/delta.md) | 18,026 | 16,674 | −7.5% | −12.9% | 6/6 | AND met in sample |
| [`fs apply: validation`](docs/benchmarks/fs/apply.md) | 17,178 | 15,768 | −8.2% | −50.7% | 6/6 | AND met in sample |
| [`fs apply: shared edits`](docs/benchmarks/fs/apply.md) | 18,583 | 17,300 | −6.9% | −1.6% | 6/6 | AND met in sample |
| [`fs test-results`](docs/benchmarks/fs/test-results.md) | 21,844 | 25,794 | +18.1% | −11.5% | 4/4 | Conditional: cache metrics conflict |

Standalone one-off use and return after other real work have not been tested separately. Cache was uncontrolled, including in the first trial; AND assessments apply only to observed sample means.

[Protocol, limits, evidence, and reproduction](docs/benchmarks/fs/README.md).

Latest review, PR return, and two apply scenarios include input authoring and final-state checks. [Scopes, cache limits, and evidence](docs/benchmarks/actionable/README.md).

Session-derived workflows report total/uncached tokens, time and actual CLI/API counts in the [completion study](docs/benchmarks/usage/README.md). JUnit overview uses two normal pairs; the malformed-XML pair is separate.

The latest changed-workflow comparison covers review source/return, large report recovery, saved-config proof and offline CI evidence. It compares complete direct/tool tasks, including authoring, recovery and final verification, while retaining unfavorable trials and separately reporting cache conflicts. [Results and evidence](docs/benchmarks/completion/README.md).

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

# Create an issue, assign yourself, and check out its linked branch in a separate worktree.
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json

# Change directory to branch.worktree_path from the result.
# Implement and verify your changes, then commit and push with Git.
tools github pr create --prefix fix --title "handle duplicate requests" --body-file pr.md --json

# Collect submitted reviews and unresolved conversations with code locations.
tools github pr reviews --number 123 --json

# Wait for checks, merge when ready, or return failure evidence in one call.
tools github pr merge --number 123 --json
```

Issue creation retrieves the metadata it needs; `context` is optional. Add `--dry-run --json` to preview a creation plan. Outside a Git checkout, pass `--repo OWNER/REPO`. Bodies default to Korean unless configured otherwise; [setup](docs/github/setup.md) saves your language and label preferences.

Local issue branches use `<main-checkout>.worktrees/<branch>` and preserve your current branch and files. Existing worktrees for the branch are reused; `--no-checkout` skips local worktree creation.

Combine routine PR work and avoid replaying unchanged evidence:

```sh
# Push existing commits, reuse/create a PR, and inspect its checks.
tools github pr submit --prefix fix --title "handle duplicate requests" --body-file pr.md --json
# Read PR state, reviews, checks, and CI failures; repeat with the same state file for changes only.
tools github pr inspect --number 123 --state-file .tools/state/pr-123.json --json
# Merge, then clean only this PR's finished branch.
tools github pr merge --number 123 --json
```

PR inspection, submission, and merging use compact output only when both bytes and tokens decrease, preserving full evidence by file path and SHA-256. Review and CI commands opt in with `--compact`. Merge continues into cleanup for that PR after confirmed success; use `--cleanup=false` to skip cleanup. [Review output measurements](docs/benchmarks/github/pr/reviews.md) cover two explicit encodings. File comparisons use `pr delta --number 123 --since FULL_SHA`; add `--include-patch` when needed. The [latest delta benchmark](docs/benchmarks/github/pr/delta.md) covers baseline verification with the default patch omission.

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

After the PR or issue closes, return to the main checkout and preview cleanup:

```sh
tools github branch cleanup --json
# Inspect the candidates, then delete them.
tools github branch cleanup --apply --json
```

Cleanup removes clean linked worktrees together with finished branches. It preserves the main/current worktree, locked or dirty worktrees, protected branches, open PR work, and unpublished local commits. Closed, unmerged remote work is also eligible; inspect the preview. [Cleanup rules and options](docs/github/branch/cleanup.md).
### Filesystem workflows

```sh
tools fs inspect --root . --include '**/*.go' --pattern cache --pattern retry --context 2 --json
tools fs inspect --request examples/fs/read-requests.json --json
# Markdown structure and a known complete section
tools fs inspect --path README.md --outline --json
tools fs inspect --path README.md --section Install --raw --hash --json
# Valid report counts and diagnostics; does not prove a test execution
tools fs test-results --include "build/test-results/**/*.xml" --json
# Original fragments for edits, including exact line terminators
tools fs inspect --request examples/fs/read-requests.json --raw --hash --json
tools fs delta --root . --include '**/*.go' --state-file .tools/state/fs/go.json --include-content --json
# Two fresh observations with one report and a preserved baseline
tools fs delta --include '**/*.go' --state-file .tools/state/fs/go.json --include-content --peek --comparisons 2 --json
tools fs apply --root . --plan edits.json --json
tools fs apply --root . --plan edits.json --apply --report-changes --json
# Capture hashes, save the plan, apply and verify an authorized spec
tools fs apply --spec replacements.json --save-plan validated-plan.json --apply --report-changes --json
# If recounting exactly this replacement was requested, include its diagnostic and recovery
tools fs apply --spec replacements.json --recount 1:1 --save-plan recounted-plan.json --apply --report-changes --json
```

No Git checkout or GitHub authentication is required. `apply` previews by default and validates the whole plan's hashes and exact replacement counts before writes, then reads applied files back to verify them. Incomplete reads return `partial` and do not advance delta state. [Options, plan format, and mutation scope](docs/fs/README.md).

## Documentation

- [GitHub command reference (Korean)](docs/github/README.md) — commands, options, configuration, and recovery.
- [GitHub agent usage (Korean)](docs/agent-usage.md) — a short workflow for coding agents.
- [Input and policy examples](examples/github) — issue/PR JSON, Markdown, and `.tools.json`.
- [Benchmarks](docs/benchmarks/README.md) — latest command measurements, raw data, and reproduction.
- [All-command audit (Korean)](docs/command-audit.md) — independent use, returning after other work, and the earlier 18-command checks and added evidence for the current 19-command set.
- [Development and leak checks](docs/development.md) · [CI report experiment](docs/ci-reports.md).

Use `tools --help` to discover commands and a command's `--help` for its options.

## Contributing

New workflow groups, bug reports, and contributions from other projects are welcome. See [Contributing](CONTRIBUTING.md), [Security](SECURITY.md), and [repository policies](docs/repository.md).

Licensed under the [MIT License](LICENSE).
