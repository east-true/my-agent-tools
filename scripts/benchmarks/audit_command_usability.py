#!/usr/bin/env python3
"""모델 호출·실제 GitHub 쓰기 없이 명령 출력의 작업 가능성을 관찰한다."""
import argparse
import hashlib
import json
import os
import platform
import subprocess
import tempfile
from datetime import datetime, timezone, timedelta
from pathlib import Path


COMMANDS = [
    "github context", "github setup", "github issue create", "github issue branch",
    "github pr create", "github pr submit", "github pr reviews", "github pr inspect",
    "github pr delta", "github pr merge", "github ci failures", "github ci rerun",
    "github dependabot list", "github dependabot view", "github branch cleanup",
    "fs inspect", "fs delta", "fs apply", "fs test-results",
]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[2]
    source = repo / "internal/cli/pr_sources_test.go"
    probe = Path(__file__).with_name("command_usability_probe.go.txt")
    with tempfile.TemporaryDirectory(prefix="my-agent-tools-usability-") as directory:
        temporary = Path(directory)
        overlay_source = temporary / "probe_test.go"
        overlay_source.write_text(source.read_text(encoding="utf-8") + "\n" + probe.read_text(encoding="utf-8"), encoding="utf-8")
        overlay = temporary / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(source): str(overlay_source)}}), encoding="utf-8")
        environment = dict(os.environ)
        environment.setdefault("GOCACHE", str(temporary / "go-cache"))
        process = subprocess.run(
            ["go", "test", "-count=1", "-overlay", str(overlay), "./internal/cli", "-run", "^TestAudit", "-v"],
            cwd=repo, env=environment, capture_output=True, text=True, encoding="utf-8",
        )
    if process.returncode:
        raise RuntimeError(process.stdout + process.stderr)
    observations = []
    for line in process.stdout.splitlines():
        if "AUDIT_OBSERVATION " in line:
            observations.append(json.loads(line.split("AUDIT_OBSERVATION ", 1)[1]))
    if len(observations) != 4:
        raise RuntimeError("expected four observations")
    paths = ['go.mod', 'go.sum']
    paths += [str(p.relative_to(repo)) for folder in ('cmd','internal') for p in sorted((repo/folder).rglob('*.go'))]
    paths += ['scripts/benchmarks/audit_command_usability.py','scripts/benchmarks/command_usability_probe.go.txt']
    report = {
        "version": 1,
        "kind": "command-usability-audit",
        "date_kst": datetime.now(timezone(timedelta(hours=9))).date().isoformat(),
        "commands": COMMANDS,
        "model_calls": 0,
        "token_measurement": False,
        "real_github_mutations": False,
        "runtime_platform": platform.system().lower(),
        "scope": "19 command help contracts; two production CLI observations and one inspection serialization observation; remaining proposals use source/docs/existing trial evidence",
        "observations": observations,
        "source_sha256": [{"path": path, "sha256": hashlib.sha256((repo / path).read_bytes()).hexdigest()} for path in paths],
        "limitations": [
            "Help checks do not establish successful execution of all 19 workflows.",
            "GitHub observations use the existing in-process fixture API; serializer observation uses a selected current review result.",
            "Large JUnit scenario uses the normal default output budget, not a small forced page limit.",
            "Observations show output/retrieval gaps; they do not measure work-token savings.",
        ],
    }
    encoded = json.dumps(report, ensure_ascii=False, indent=2) + "\n"
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(encoded, encoding="utf-8")
    print(json.dumps({"commands": len(COMMANDS), "observations": observations, "model_calls": 0}, ensure_ascii=False))


if __name__ == "__main__":
    main()
