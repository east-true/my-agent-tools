# my-agent-tools

[한국어](docs/README.ko.md) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md)

An open-source, cross-platform CLI for automating repeatable agent workflows. It is designed for use across projects and by different users, with repository-specific policies configured separately.

The binary is named `tools`. The first command group, `tools github`, automates GitHub issues, linked development branches, and pull requests. Additional workflows can be added as separate command groups.

## Build and authentication

Building requires Go 1.26 or newer. The resulting binary does not require a Go runtime.

```sh
go build -o tools ./cmd/tools
```

On Windows, use `go build -o tools.exe ./cmd/tools`. Add the binary directory to your PATH, or run `./tools` (`./tools.exe` in PowerShell).

GitHub API calls use the Go SDK directly. Authentication checks `GH_TOKEN`, then `GITHUB_TOKEN`, then an optional existing login through `gh auth token`. GitHub CLI is not required when a token is supplied. Only `github.com` is currently supported.

Git is used to discover the current repository and branch and to check out linked branches. Use `--repo OWNER/REPO --no-checkout` to create an issue and remote branch without Git. Creating a PR without Git requires an explicit repository and JSON `head` field.

## Quick start

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

Include the changes and actual verification results in the PR body. The tool does not run verification commands or commit or push changes.

Issue bodies are checked for Korean text by default. For English or other languages, put this policy in the target repository's `.tools.json`:

```json
{
  "github": {
    "body_language": "any"
  }
}
```

When an option is unclear, read that command's `--help`, such as `tools github issue create --help`. Top-level, GitHub, issue/PR, and creation help list the supported prefixes. [Agent usage](docs/agent-usage.md) provides a short workflow reference.

## Inputs and repository policy

Supported prefixes are `feat`, `fix`, `refactor`, `docs`, `ci`, `test`, `chore`, `build`, `perf`, `style`, and `revert`. Titles accept English letters, digits, ASCII punctuation, and spaces, with at least one letter or digit. The authored title's case is preserved; the prefix is added when missing. Branch slugs are converted to lowercase kebab-case and limited to 180 characters.

Use `--title` and `--body-file` for authored Markdown, or `--file` for structured JSON. Both accept `-` for standard input where applicable. Do not combine JSON file input with title/body options. Examples: [issue JSON](examples/github/issue.json), [issue Markdown](examples/github/issue.md), [PR JSON](examples/github/pr.json), and [policy](examples/github/tools-config.json).

Structured issue bodies use `summary`, `changes`, and `acceptance`. PR bodies use `summary`, `changes`, and `verification`. Verification results are `passed`, `failed`, or `not-run`; the latter two require `details`. Omitted verification is recorded as not run. Direct `body` input cannot be combined with structured body fields.

The policy file defaults to `.tools.json` at the current Git root, or the current directory outside Git. Use `--config FILE` to override it. Policies can customize label and issue-type candidates:

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

## Token usage measurements

In a read-only issue/branch planning comparison with usage guides and Git checkouts, three trials per method averaged 46,630 input-plus-output tokens for direct `gh` and 46,279 for `tools` (0.75% lower). Uncached input was effectively equal: 16,035 versus 16,063 tokens. Both methods used two shell calls, with `gh` batching its API commands. This small experiment does not establish meaningful token savings or measure actual issue/branch/PR creation. See the [guided benchmark results and prompts](docs/benchmarks/github-token-usage-guided.json) and the [earlier comparison without usage guides](docs/benchmarks/github-token-usage.json).

An additional [candidate validation](docs/benchmarks/github-candidate-validation.json) used 18 model trials over fixed public GitHub snapshots. CI diagnostic extraction reduced total tokens by 13.7% relative to reading the failed-step log; review extraction reduced 1.2%, and incremental PR retrieval increased 3.0%. Filtered review/compare outputs are already available with `gh --jq`. These are offline prototypes under `scripts/benchmarks` and are not `tools github` commands.

Six [follow-up CI trials](docs/benchmarks/github-ci-internal-validation.json) measured a stronger `gh + rg` baseline and deterministic internal parsing. Mean total usage was 31,658 tokens for `gh + rg` and 30,244 for internal facts returned to the agent (4.5% lower). The internal code also returned the bounded CI/review/delta reference answers without model calls; that component uses zero model tokens, excluding an outer agent invocation. The prototypes have 21 boundary checks. `python3 scripts/benchmarks/github_candidates.py ci-facts LOG_FILE` extracts supported Go compiler facts and retains original evidence for unsupported formats.

A [repeated code-repair experiment](docs/benchmarks/github-workflow-validation.json) tested two actual Go fixes across three workflows per method, with independent tests. Mean total tokens were 96,631 for a log-reading agent, 94,441 for an agent given reports/code, and 30,362 for a model that returned code while the caller applied and tested it. The last mode processed 68.6% fewer total tokens, but had zero cached input and more uncached input (30,018 versus 9,658); monetary savings are not established. All 18 fixes passed. Every method shared the same native gate, which handled six duplicate/passed events per eight observations without calling a model.

## Structured CI report experiment

CI captures `go test -json` into a versioned `ci-report.json`, retaining failed-test evidence and package/build failures. Each OS report is uploaded as `go-ci-<os>-<attempt>` only after Gitleaks passes. A missing or incomplete result is kept visible. This workflow configuration has been checked locally; artifact upload has not yet been exercised on GitHub.

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
