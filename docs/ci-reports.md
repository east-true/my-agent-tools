# Structured CI report experiment

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


See the [benchmark report](benchmarks/github/workflow.md) for measured code-repair workflows.
