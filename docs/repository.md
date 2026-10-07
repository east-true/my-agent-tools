# Repository policies

The project is an open-source CLI for users and contributors across repositories. Personal account preferences belong in local configuration, not shared defaults tied to a maintainer.

Contribution expectations are in [Contributing](../CONTRIBUTING.md). The [code of conduct](../CODE_OF_CONDUCT.md) defines community behavior; the [security policy](../SECURITY.md) covers vulnerability reporting.

## GitHub settings

- Public repository with the existing MIT License.
- Issues for bugs and feature proposals; Discussions for questions and workflow ideas.
- Documentation is versioned in the repository rather than in the wiki.
- Squash merging uses the PR title and body; merged branches are deleted automatically. Auto-merge can be enabled per PR once required checks pass. The Update branch button is available to bring a PR up to date with its base branch.
- Actions have read-only default permissions and cannot approve PRs.
- Actions from fork PRs by any external contributor require maintainer approval before running, including returning contributors. Approval lets the workflow run; it does not approve or merge the PR. See [GitHub's workflow approval documentation](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository).
- Dependabot alerts/security updates, private vulnerability reporting, secret scanning, and push protection are enabled. Scheduled dependency-update configuration is in `.github/dependabot.yml`.

Non-provider pattern scanning and automatic validity checks remain disabled: GitHub did not enable them when requested for this user-owned public repository. Validity checks for partner patterns require [GitHub Secret Protection on an eligible organization plan](https://docs.github.com/en/code-security/how-tos/secure-your-secrets/customize-leak-detection/enable-validity-checks). The settings file includes only the supported features verified on this repository; CI also scans with Gitleaks.

`.github/repository-settings.json` contains the repository settings accepted by GitHub's [Update a repository API](https://docs.github.com/en/rest/repos/repos#update-a-repository). An administrator can apply the reviewed settings with:

```sh
gh api --method PATCH repos/east-true/my-agent-tools --input .github/repository-settings.json
gh api --method PUT repos/east-true/my-agent-tools/actions/permissions/workflow \
  -f default_workflow_permissions=read -F can_approve_pull_request_reviews=false
gh api --method PUT repos/east-true/my-agent-tools/actions/permissions/fork-pr-contributor-approval \
  -f approval_policy=all_external_contributors
gh api --method PUT repos/east-true/my-agent-tools/vulnerability-alerts
gh api --method PUT repos/east-true/my-agent-tools/automated-security-fixes
gh api --method PUT repos/east-true/my-agent-tools/private-vulnerability-reporting
```

These commands change live GitHub settings; committing the files alone does not apply them. Verify the repository and Actions settings with GET requests after applying them. Visibility and licensing are reviewed separately from this settings file.

## Default branch rules

`.github/rulesets/main.json` is the versioned definition of the `default-branch` ruleset. Changes to the default branch require a PR, resolved review threads, and passing checks against the latest base. Force pushes and branch deletion are blocked. There are no bypass actors and no mandatory second reviewer while the project has a small maintainer team.

Required GitHub Actions check names are `gitleaks`, `test (ubuntu-latest)`, `test (macos-latest)`, `test (windows-latest)`, and `cross-build`. Keep these names and the ruleset synchronized when editing CI. The first PR containing the workflow must pass these checks before merging.

GitHub settings are managed separately from Git contents. Updating the JSON file does not automatically change the live ruleset; maintainers must apply the reviewed definition through the repository settings or GitHub API.

## Local agent files

Git ignores `AGENTS.md`, `CLAUDE.md`, `CODEX.md` (including lowercase spellings and local variants), `.agents/`, `.claude/`, `.codex/`, and Claude's local JSON configuration. Shared documentation in `docs` and reusable examples remain tracked.

Ignore rules prevent new files from being added normally; they do not remove files from existing commits. Review tracked files before publishing, and avoid force-adding local instruction files.
