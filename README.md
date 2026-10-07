# my-agent-tools

[한국어](docs/README.ko.md) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md)

An open-source, cross-platform CLI for automating repeatable agent workflows. It is designed for use across projects and by different users, with repository-specific policies configured separately.

The binary is named `tools`. The first command group, `tools github`, automates GitHub issues, linked development branches, pull requests, and cleanup of finished branches. Additional workflows can be added as separate command groups.

Branch cleanup used **56.1% fewer total agent tokens** than direct `gh + git` in a controlled 14-branch fixture: classify finished work, preserve active/unpublished work, and delete matching refs. See the [per-command measurements and evidence](#token-usage-measurements).

## Build and authentication

Building requires Go 1.26 or newer. The resulting binary does not require a Go runtime.

```sh
go build -o tools ./cmd/tools
```

On Windows, use `go build -o tools.exe ./cmd/tools`. Add the binary directory to your PATH, or run `./tools` (`./tools.exe` in PowerShell).

GitHub API calls use the Go SDK directly. Authentication checks `GH_TOKEN`, then `GITHUB_TOKEN`, then an optional existing login through `gh auth token`. GitHub CLI is not required when a token is supplied. Only `github.com` is currently supported.

Git is used to discover the current repository and branch and to check out linked branches. Use `--repo OWNER/REPO --no-checkout` to create an issue and remote branch without Git. Creating a PR without Git requires an explicit repository and JSON `head` field.

## Quick start

Optionally initialize project policy from the repository's current labels and issue types:

```sh
tools github setup
tools github setup --dry-run --json   # inspect available names and proposed config
```

Create a UTF-8 Markdown body file, then run:

```sh
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json
```

The command reads existing labels, issue types, and Markdown templates, assigns the authenticated user, and creates the issue. For issue 123 it creates `123-fix-handle-duplicate-requests`, associates it with the issue's Development section, and checks it out when the current origin matches the target repository.

For a plan without writes, add `--dry-run --json`. Creation commands retrieve and validate the required catalog themselves, so `context` is optional. Use the returned plan or result; repeat catalog lookups only when an error or missing detail requires it. Outside a Git checkout, pass `--repo OWNER/REPO`.

After making changes, commit and push with Git, then create a PR:

```sh
tools github pr create --prefix fix --title "handle duplicate requests" --body-file pr.md --json
```

Include the changes and actual verification results in the PR body. Creation commands do not run verification commands or commit or push changes.

Issue bodies are checked for Korean text by default. For English or other languages, put this policy in the target repository's `.tools.json`:

```json
{
  "github": {
    "body_language": "any"
  }
}
```

When an option is unclear, read that command's `--help`, such as `tools github issue create --help`. Top-level, GitHub, issue/PR, and creation help list the supported prefixes. [Agent usage](docs/agent-usage.md) provides a short workflow reference.

The [command reference (Korean)](docs/github/README.md) covers [setup](docs/github/setup.md), [context](docs/github/context.md), [issue create](docs/github/issue/create.md), [issue branch](docs/github/issue/branch.md), [PR create](docs/github/pr/create.md), and [branch cleanup](docs/github/branch/cleanup.md).

## Inputs and repository policy

Supported prefixes are `feat`, `fix`, `refactor`, `docs`, `ci`, `test`, `chore`, `build`, `perf`, `style`, and `revert`. Titles accept English letters, digits, ASCII punctuation, and spaces, with at least one letter or digit. The authored title's case is preserved; the prefix is added when missing. Branch slugs are converted to lowercase kebab-case and limited to 180 characters.

Use `--title` and `--body-file` for authored Markdown, or `--file` for structured JSON. Both accept `-` for standard input where applicable. Do not combine JSON file input with title/body options. Examples: [issue JSON](examples/github/issue.json), [issue Markdown](examples/github/issue.md), [PR JSON](examples/github/pr.json), and [policy](examples/github/tools-config.json).

Structured issue bodies use `summary`, `changes`, and `acceptance`. PR bodies use `summary`, `changes`, and `verification`. Verification results are `passed`, `failed`, or `not-run`; the latter two require `details`. Omitted verification is recorded as not run. Direct `body` input cannot be combined with structured body fields.

The policy file defaults to `.tools.json` at the current Git root, or the current directory outside Git. Use `--config FILE` to override it. `tools github setup` fetches existing names and saves concrete prefix mappings there. Existing preferences, including body language and explicitly empty mappings, are preserved; missing prefixes are filled. `--refresh` recalculates supported mappings, while explicit `--set-label`/`--set-issue-type` choices take precedence:

```sh
tools github setup --set-label feat=enhancement --set-label fix=bug
tools github setup --set-issue-type fix=Bug --body-language any
tools github setup --refresh --dry-run --json
tools github setup --repo OWNER/REPO --config project.tools.json
```

Names supplied with `--set` must already exist; issue types must be enabled. Repeat the same prefix to save ordered candidates, or use `--set-label ci=` to disable that mapping. Auto-matching uses the same existing-name rules as creation, including common `type:`/`kind/` namespaces. Unmatched prefixes get empty candidate arrays and notes; project-specific labels can be chosen explicitly. No GitHub labels or types are created or updated. Setup refuses malformed configs and checks for edits made while fetching before saving. Repeating setup with unchanged preferences produces the same settings.

Policies can also be edited directly:

```json
{
  "github": {
    "body_language": "any",
    "label_map": {"feat": ["type: feature", "enhancement"]},
    "issue_type_map": {"feat": ["Feature", "Task"]}
  }
}
```

Candidates are matched in order, case-insensitively, against existing names. Only configured prefixes replace the built-in mappings. Missing matches are reported and omitted; the tool never creates labels or issue types. Explicit JSON `labels` or `issue_type` values must exist. Assignees are always the authenticated user. Natural-language `AGENTS.md` files are not interpreted as policy.

The tool selects existing Markdown issue/PR templates by prefix or a default filename. It preserves authored content, reuses template sections, and supports a `{{body}}` placeholder. YAML issue forms are not currently interpreted.

## Branches, PRs, and recovery

Issue branches start from the repository's default branch and follow `<issue-number>-<prefix>-<slug>`. Use `--no-branch` to create only the issue, or `--no-checkout` to skip local checkout. Local branches are never forcibly overwritten.

PRs infer an issue number from the current branch and add `Closes #123`, or accept an explicit JSON `issue`. The base defaults to the repository's default branch. Heads must follow the issue-branch pattern, or `<prefix>/<slug>` without an issue. Fork heads can use `owner:branch`. The remote head must have commits ahead of the base, so commit and push before creating a PR.

If branch creation, association, or checkout fails after creating an issue, continue with:

```sh
tools github issue branch --number 123
```

Existing linked branches are reused. Unlinked branches with the same name are not overwritten. If metadata application fails after creating an issue or PR, the result preserves its URL and reports `partial`. Fix that existing item instead of creating a duplicate. Creation requests are not automatically retried; verify remote state if a response was lost.

Use `--json` for structured output. Result statuses include `ok`, `planned`, `created`, `partial`, and `error`. Exit codes are `0` for success, `1` for authentication/API/remote-state or post-creation errors, and `2` for argument, JSON, or policy errors. `--dry-run` performs reads and preflight checks without remote writes. `tools github context` is an optional catalog command; creation commands retrieve the required catalog themselves.

## Clean up finished branches

```sh
tools github branch cleanup --json           # read-only preview
tools github branch cleanup --apply --json   # delete eligible branches
```

The command inspects both local branches and branches currently on the selected GitHub remote. A branch qualifies when its latest PR is merged or closed, or an associated issue is closed. Issues are associated through Development links or the `<issue-number>-<type>-<slug>` name. Open PRs take precedence over closed issues. Fork PRs with the same branch name are ignored, and a remote branch with commits newer than its closed PR is retained.

Default, `main`, `master`, GitHub-protected, and checked-out worktree branches are excluded. Local aliases protect the branches they track. Local branches with commits not verified as published are kept; squash merges are supported through the PR head commit. Stale remote-tracking refs for qualifying branches that no longer exist remotely are cleaned too. Missing local commit objects cause the local branch to be kept rather than fetched implicitly.

Use `--remote upstream` for another configured Git remote, `--scope local` or `--scope remote` to restrict cleanup, and `--protect 'develop,release/*'` to exclude additional names or globs. The default scope is `both`. Output includes candidates/actions and counts; add `--include-skipped` to inspect kept branches and reasons. The selected remote's fetch and single push URL must match the target repository, including when `--repo` is supplied. Remote deletion uses Git's configured transport credentials.

`--apply` also deletes closed but unmerged remote work, so inspect the preview. Applying refreshes GitHub states and protections, rechecks worktrees, and deletes refs only at the expected commit SHA. Failures preserve per-branch results; failed remote deletion retains its local branch. Cleanup does not switch branches, alter working files, or commit changes. Avoid concurrent Git writers: GitHub issue/PR states and worktree changes cannot be locked atomically with a ref deletion. After a remote Development ref disappears, a local branch needs a PR association or issue-number name to recover its issue association.

## Token usage measurements

Browse the [benchmark index](docs/benchmarks/README.md) for command-specific reports and raw evidence.

These are mean tokens for the entire measured agent task, including instructions, tool interactions, cached input, and output. The CLI itself does not call a model. Both methods used `gpt-6.1-sol` with high reasoning effort and three fresh sessions per measured method; direct `gh` could batch commands and filter locally.

| Command | Compared task | Direct `gh` / `gh + git` | `tools` | Total token change | Evidence |
|---|---|---:|---:|---:|---|
| `github context` | Independent catalog lookup | — | — | Not measured separately | [Scope](docs/benchmarks/github/context.md) |
| `github setup` | Fetch metadata and save project mappings | — | — | Not measured | — |
| `github issue create --dry-run` | Read-only issue + linked-branch plan | 46,630 | 46,279 | −0.75% | [3 trials per method](docs/benchmarks/github/issue-create.md) |
| `github issue create` | Actual issue + linked-branch creation | — | — | Not measured | — |
| `github issue branch` | Create/resume an existing issue's branch | — | — | Not measured separately | — |
| `github pr create` | Actual PR creation | — | — | Not measured | — |
| `github branch cleanup` | Preview only | — | — | Not measured separately | — |
| `github branch cleanup --apply` | Classify and actually delete fixture refs | 70,552 | 31,006 | **−56.1%** | [3 trials per method](docs/benchmarks/github/branch-cleanup.md) |

For issue planning, uncached input was effectively equal: 16,035 for `gh` versus 16,063 for `tools`. Both used two shell calls. This does not establish meaningful savings for issue creation; [the earlier comparison without usage guides](docs/benchmarks/github/issue-create.md#사용-안내-없는-초기-측정) also records discovery overhead.

For branch cleanup, uncached input averaged 28,414 versus 16,102 (43.3% lower), and shell command items averaged 5.33 versus 1. Each valid trial removed six local branches, five remote branches, and two stale tracking refs while retaining 15 targets. Final refs, SHA deletion guards, branch configuration, worktree and working-file preservation matched in all six compared runs. The tool completed classification and guarded deletion internally instead of having the agent write and execute that policy.

The cleanup experiment used real `gh`, the production CLI runner/cleanup source with API dependency injection, a local GitHub fixture API, and real disposable Git repositories. Its initial direct-method guide ambiguously scoped one protection; all three initial direct runs are retained in the report and excluded from the equal-workload comparison. The clarified direct guide was measured three more times against the unchanged valid `tools` trials. Cache was uncontrolled and the corrected baseline ran later; this is a small fixture experiment, not proof of monetary savings or a guarantee for other commands. Implementation/setup/analysis costs are excluded. A reusable equivalent skill/script could provide the same native automation.

To reproduce on Linux/WSL, install Go, Git, `gh`, and Codex. The first command runs a model-free fixture/sandbox preflight; the second starts up to six model executions. Use fresh output directories:

```sh
python3 scripts/benchmarks/run_branch_cleanup_benchmark.py --root /tmp/branch-cleanup-preflight
python3 scripts/benchmarks/run_branch_cleanup_benchmark.py --root /tmp/branch-cleanup-experiment --run-models
```

An additional [candidate validation](docs/benchmarks/github/ci.md#초기-후보-측정) used 18 model trials over fixed public GitHub snapshots. CI diagnostic extraction reduced total tokens by 13.7% relative to reading the failed-step log; review extraction reduced 1.2%, and incremental PR retrieval increased 3.0%. Filtered review/compare outputs are already available with `gh --jq`. These are offline prototypes under `scripts/benchmarks` and are not `tools github` commands.

Six [follow-up CI trials](docs/benchmarks/github/ci.md#내부-파서-후속-측정) measured a stronger `gh + rg` baseline and deterministic internal parsing. Mean total usage was 31,658 tokens for `gh + rg` and 30,244 for internal facts returned to the agent (4.5% lower). The internal code also returned the bounded CI/review/delta reference answers without model calls; that component uses zero model tokens, excluding an outer agent invocation. The prototypes have 21 boundary checks. `python3 scripts/benchmarks/github_candidates.py ci-facts LOG_FILE` extracts supported Go compiler facts and retains original evidence for unsupported formats.

A [repeated code-repair experiment](docs/benchmarks/github/workflow.md) tested two actual Go fixes across three workflows per method, with independent tests. Mean total tokens were 96,631 for a log-reading agent, 94,441 for an agent given reports/code, and 30,362 for a model that returned code while the caller applied and tested it. The last mode processed 68.6% fewer total tokens, but had zero cached input and more uncached input (30,018 versus 9,658); monetary savings are not established. All 18 fixes passed. Every method shared the same native gate, which handled six duplicate/passed events per eight observations without calling a model.

## Structured CI report experiment

CI captures `go test -json` into a versioned `ci-report.json`, retaining failed-test evidence and package/build failures. Each OS report is uploaded as `go-ci-<os>-<attempt>` only after Gitleaks passes. A missing or incomplete result is kept visible. Linux, macOS, and Windows jobs and all three uploaded reports were verified in a [real GitHub CI run](https://github.com/east-true/my-agent-tools/actions/runs/37579126000). Downloaded reports matched the run, job, repository, and PR merge revision. The native gate handled each passing report and its acknowledged replay without model calls; failure-to-repair model trials remain local experiments.

The report helper requires Python 3.9 or newer and is an experimental script, separate from the Go binary. It tracks added, changed, and resolved failures by repository/job/revision/run. Preparing a report does not acknowledge it:

```sh
python scripts/benchmarks/workflow_reports.py prepare --report artifacts/ci-report.json --state .tools/state/ci.json --expected-revision EXPECTED_CI_SHA --context-root . --include path/to/source.go
```

Call a model only for work requiring interpretation or code changes. A code-proposal worker can return a bounded replacement, then native code verifies the source hash, applies/formats it, and runs independent tests. Acknowledge failed work only after verification; incomplete reports cannot be acknowledged. The state protocol assumes a single consumer. Other repositories can supply the same JSON format; annotation/log adapters are not implemented in this experiment.

To reproduce the controlled model experiments, use fresh output directories. The first command starts up to 12 model executions; the second starts up to six:

```sh
python scripts/benchmarks/run_workflow_benchmark.py --output /tmp/workflow-experiment --run-models
python scripts/benchmarks/run_worker_benchmark.py --output /tmp/worker-experiment --cache /tmp/workflow-experiment/cache
```

## Development and leak checks

```sh
go test ./...
go vet ./...
go run ./scripts/build
```

Tests use isolated HTTP servers instead of creating real GitHub issues or PRs. CI tests, vets, and builds on Linux, macOS, and Windows. The build script produces six CGO-free amd64/arm64 binaries under `dist` and `dist/SHA256SUMS`.

CI also runs Gitleaks v8.30.1 on Git file-change history and the checkout. Default secret rules are extended with email, Korean mobile phone, and resident/foreign registration number patterns. Findings fail the job, and detected values are redacted:

```sh
go install github.com/zricethezav/gitleaks/v8@v8.30.1
gitleaks git --config .gitleaks.toml --redact --no-banner --log-opts="--all --full-history" .
gitleaks dir --config .gitleaks.toml --redact --no-banner .
```

Reserved example domains, GitHub noreply addresses, and known Git SSH transport users are allowed only for the email rule. Test source files are scanned. Local `.git` metadata, generated CLI binaries, and Python bytecode caches are excluded from the directory scan. Commit author metadata and free-form names/addresses are not inspected; use a GitHub noreply commit email if desired.

Local agent instructions and configuration (`AGENTS.md`, `CLAUDE.md`, `CODEX.md`, `.agents/`, `.claude/`, and `.codex/`) are ignored by Git. Shared user documentation and examples remain versioned.

## Community and license

Bug reports and feature proposals are welcome through Issues. Use Discussions for questions and workflow ideas, and private vulnerability reporting for security concerns. See [CONTRIBUTING.md](CONTRIBUTING.md) for development and PR expectations and [repository policies](docs/repository.md) for maintainer settings.

Licensed under the [MIT License](LICENSE).
