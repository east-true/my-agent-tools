# Security policy

The project is under development. Security fixes target the current default branch; older snapshots do not have a separate support commitment.

## Report a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/east-true/my-agent-tools/security/advisories/new) to report suspected vulnerabilities privately. Include the affected revision, reproduction steps, and potential impact. Use synthetic examples and redact credentials and personal data. Do not put exploit details or live secrets in public issues or discussions.

Maintainers will assess the report and coordinate a fix and disclosure with the reporter. No guaranteed response timeframe is currently published.

## Credentials and personal information

Provide GitHub credentials through environment variables or an existing GitHub CLI login. Do not commit tokens or include them in issue/PR input files. If a live credential has been exposed, revoke or rotate it; deleting its latest occurrence does not remove it from Git history.

CI uses Gitleaks default secret rules and additional patterns for email addresses, Korean mobile numbers, and registration numbers. These checks are pattern-based and do not identify every kind of personal information. Git author metadata is outside the file-content scan. GitHub secret scanning and push protection provide additional repository checks.
