# Contributing

Contributions from different users, projects, and agent environments are welcome. Keep workflows reusable: expose repository-specific preferences through policy or options rather than hardcoding a maintainer's account, paths, language, or tools.

Use Issues for reproducible bugs and concrete feature proposals, Discussions for questions, and [private vulnerability reporting](SECURITY.md) for security concerns. English and Korean reports are welcome. Be respectful, focus feedback on the work, and avoid sharing other people's personal information.

## Development

Install Go 1.26 or newer and Git, clone the repository or your fork, and build:

```sh
go build -o tools ./cmd/tools
go test ./...
go vet ./...
```

Tests use local HTTP servers; a GitHub token is not required. Changes to platform support should also pass `go run ./scripts/build`. Keep Windows, macOS, and Linux compatibility in mind. Add meaningful tests for changed behavior and format Go code with `gofmt`.

For local policy choices, use `.tools.json` or `--config`. Example configuration belongs in `examples`; avoid adding personal agent instructions, account data, credentials, or machine-specific paths. `AGENTS.md`, `CLAUDE.md`, `CODEX.md` and their local configuration directories are ignored by Git.

## Pull requests

- Use a lowercase English title such as `fix: handle duplicate requests` or `feat: add a workflow`.
- Name issue branches `<issue-number>-<type>-<slug>` and other branches `<type>/<slug>` using lowercase kebab-case.
- Explain the problem, resulting behavior, and actual verification. Include `Closes #123` when appropriate.
- Update usage documentation and examples when public behavior changes.
- Run the [Gitleaks checks](docs/development.md#leak-checks) before submitting. Use reserved example domains for fixtures and redact logs.

Changes to the default branch go through a PR with passing CI and resolved review conversations. PRs use squash merging. Additional reviewer approval is welcome; the initial policy does not require a second maintainer.

By contributing, you agree that your contributions are provided under this project's MIT License.
