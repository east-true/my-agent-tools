# Development

```sh
go test ./...
go vet ./...
go run ./scripts/build
```

Tests use isolated HTTP servers instead of creating real GitHub issues or PRs. CI tests, vets, and builds on Linux, macOS, and Windows. The build script produces six CGO-free amd64/arm64 binaries under `dist` and `dist/SHA256SUMS`.

## Leak checks

CI also runs Gitleaks v8.30.1 on Git file-change history and the checkout. Default secret rules are extended with email, Korean mobile phone, and resident/foreign registration number patterns. Findings fail the job, and detected values are redacted:

```sh
go install github.com/zricethezav/gitleaks/v8@v8.30.1
gitleaks git --config .gitleaks.toml --redact --no-banner --log-opts="--all --full-history" .
gitleaks dir --config .gitleaks.toml --redact --no-banner .
```

Reserved example domains, GitHub noreply addresses, and known Git SSH transport users are allowed only for the email rule. Test source files are scanned. Local `.git` metadata, generated CLI binaries, and Python bytecode caches are excluded from the directory scan. Commit author metadata and free-form names/addresses are not inspected; use a GitHub noreply commit email if desired.

Local agent instructions and configuration (`AGENTS.md`, `CLAUDE.md`, `CODEX.md`, `.agents/`, `.claude/`, and `.codex/`) are ignored by Git. Shared user documentation and examples remain versioned.


See [Contributing](../CONTRIBUTING.md) for pull request expectations and [repository policies](repository.md) for maintainer settings.
