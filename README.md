# my-agent-tools

**GitHub workflows in one command, for coding agents and developers.**

[한국어](docs/README.ko.md) · [Commands](docs/github/README.md) · [Benchmarks](docs/benchmarks/README.md) · [Contributing](CONTRIBUTING.md)

`tools` handles repeatable GitHub work inside a cross-platform CLI:

- **Issues → branches:** match existing labels and issue types, assign `@me`, and create a linked development branch.
- **Pull requests:** infer the linked issue from your branch and apply repository policy.
- **Branch cleanup:** inspect finished work and remove eligible local and remote branches after a preview.

```sh
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json
```

For issue #123, this creates `fix: handle duplicate requests` and checks out `123-fix-handle-duplicate-requests` when the current origin matches the repository. Each repository can configure its own mappings in `.tools.json`.

## Token usage measurements

Branch cleanup used 56.1% fewer total agent tokens than direct `gh + git` in a controlled 14-branch experiment. Issue planning showed little difference.

| Command | Direct `gh` / `gh + git` | `tools` | Total token change |
|---|---:|---:|---:|
| [`github context`](docs/github/context.md) | — | — | Not measured separately |
| [`github setup`](docs/github/setup.md) | — | — | Not measured |
| [`github issue create --dry-run`](docs/benchmarks/github/issue-create.md) | 46,630 | 46,279 | −0.75% |
| [`github issue create`](docs/github/issue/create.md) | — | — | Not measured |
| [`github issue branch`](docs/github/issue/branch.md) | — | — | Not measured separately |
| [`github pr create`](docs/github/pr/create.md) | — | — | Not measured |
| [`github branch cleanup`](docs/github/branch/cleanup.md) | — | — | Preview not measured separately |
| [`github branch cleanup --apply`](docs/benchmarks/github/branch-cleanup.md) | 70,552 | 31,006 | −56.1% |

Means for the whole agent task, including cached input and output: `gpt-6.1-sol`, high reasoning, three fresh sessions per method. The CLI itself makes no model calls. Cleanup trials used a local fixture API and real disposable Git repositories; uncached input fell 43.3%, and all six compared runs matched the expected deletion and preservation results. Cache was uncontrolled; these results do not establish monetary savings or savings for other commands. [Raw evidence, limitations, and reproduction](docs/benchmarks/README.md).

## Install

From a source checkout, build with **Go 1.26+**:

```sh
go build -o tools ./cmd/tools
```

On Windows, use `go build -o tools.exe ./cmd/tools`. Add the binary directory to your PATH. The binary runs on Linux, macOS, and Windows without a Go runtime; Git is needed for local branch operations.

For GitHub authentication, use `GH_TOKEN` or `GITHUB_TOKEN`, or reuse an existing `gh auth login`. With an environment token, `gh` is optional. Currently supports `github.com`.

## Quick start

Run inside your target repository. Save your issue and PR descriptions as UTF-8 Markdown files, `issue.md` and `pr.md`.

```sh
# Import existing labels/types and allow bodies in any language.
tools github setup --body-language any

# Create an issue, assign yourself, and check out its linked branch.
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json

# Implement and verify your changes, then commit and push with Git.
tools github pr create --prefix fix --title "handle duplicate requests" --body-file pr.md --json
```

Issue creation retrieves the metadata it needs; `context` is optional. Add `--dry-run --json` to preview a creation plan. Outside a Git checkout, pass `--repo OWNER/REPO`. Bodies default to Korean unless configured otherwise; [setup](docs/github/setup.md) saves your language and label preferences.

After the PR or issue closes, switch away from the finished branch and preview cleanup:

```sh
tools github branch cleanup --json
# Inspect the candidates, then delete them.
tools github branch cleanup --apply --json
```

Cleanup preserves protected branches, branches checked out in worktrees, open PR work, and unpublished local commits. Closed, unmerged remote work is also eligible; inspect the preview. [Cleanup rules and options](docs/github/branch/cleanup.md).

## Documentation

- [Command reference (Korean)](docs/github/README.md) — all six commands, options, configuration, and recovery.
- [Agent usage (Korean)](docs/agent-usage.md) — a short workflow for coding agents.
- [Input and policy examples](examples/github) — issue/PR JSON, Markdown, and `.tools.json`.
- [Benchmarks](docs/benchmarks/README.md) — command measurements, raw data, and experimental workflows.
- [Development and leak checks](docs/development.md) · [CI report experiment](docs/ci-reports.md).

Use `tools github --help` or a command's `--help` for supported prefixes and options.

## Contributing

Bug reports, feature proposals, and contributions from other projects are welcome. See [Contributing](CONTRIBUTING.md), [Security](SECURITY.md), and [repository policies](docs/repository.md).

Licensed under the [MIT License](LICENSE).
