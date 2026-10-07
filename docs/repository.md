# Repository policies

The project is an open-source CLI for users and contributors across repositories. Personal account preferences belong in local configuration, not shared defaults tied to a maintainer.

## GitHub settings

- Public repository with the existing MIT License.
- Issues for bugs and feature proposals; Discussions for questions and workflow ideas.
- Documentation is versioned in the repository rather than in the wiki.
- Squash merging uses the PR title and body; merged branches are deleted automatically. Auto-merge can be enabled per PR once required checks pass.
- Actions have read-only default permissions and cannot approve PRs.
- Dependabot alerts/security updates, private vulnerability reporting, secret scanning, and push protection are enabled. Scheduled dependency-update configuration is in `.github/dependabot.yml`.

## Default branch rules

`.github/rulesets/main.json` is the versioned definition of the `default-branch` ruleset. Changes to the default branch require a PR, resolved review threads, and passing checks against the latest base. Force pushes and branch deletion are blocked. There are no bypass actors and no mandatory second reviewer while the project has a small maintainer team.

Required GitHub Actions check names are `gitleaks`, `test (ubuntu-latest)`, `test (macos-latest)`, `test (windows-latest)`, and `cross-build`. Keep these names and the ruleset synchronized when editing CI. The first PR containing the workflow must pass these checks before merging.

GitHub settings are managed separately from Git contents. Updating the JSON file does not automatically change the live ruleset; maintainers must apply the reviewed definition through the repository settings or GitHub API.

## Local agent files

Git ignores `AGENTS.md`, `CLAUDE.md`, `CODEX.md` (including lowercase spellings and local variants), `.agents/`, `.claude/`, `.codex/`, and Claude's local JSON configuration. Shared documentation in `docs` and reusable examples remain tracked.

Ignore rules prevent new files from being added normally; they do not remove files from existing commits. Review tracked files before publishing, and avoid force-adding local instruction files.
