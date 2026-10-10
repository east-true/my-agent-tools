#!/usr/bin/env python3
"""승인된 정수 증감을 조회·계획·반영·검증까지 한 로컬 실행으로 연결한다."""
import argparse
import hashlib
import json
import re
import subprocess


def call(arguments, data=None):
    process = subprocess.run(
        ["tools", "fs", *arguments, "--json"],
        input=json.dumps(data, ensure_ascii=False) if data is not None else None,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    value = json.loads(process.stdout)
    if process.returncode or not value.get("complete"):
        raise RuntimeError(value)
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".")
    parser.add_argument("--path", required=True)
    parser.add_argument("--key", required=True)
    parser.add_argument("--amount", type=int, required=True)
    parser.add_argument("--expect", action="append", default=[], help="unchanged KEY=INTEGER precondition; repeatable")
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", args.key):
        parser.error("key must be an ASCII identifier")
    result = call(["inspect", "--root", args.root, "--path", args.path, "--raw", "--hash"])
    if len(result["files"]) != 1:
        raise RuntimeError("expected exactly one complete source")
    file = result["files"][0]
    ranges = file["ranges"]
    if len(ranges) != 1 or ranges[0]["start"] != 1:
        raise RuntimeError("expected the complete raw file")
    original = ranges[0]["text"].encode("utf-8")
    if hashlib.sha256(original).hexdigest() != file["sha256"]:
        raise RuntimeError("raw source SHA mismatch")
    preserved = {}
    for condition in args.expect:
        key, separator, number = condition.partition("=")
        if not separator or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key) or not re.fullmatch(r"[0-9]+", number) or key == args.key or key in preserved:
            parser.error("expect must name a distinct unchanged KEY=INTEGER")
        found = re.findall(rb"(?m)^" + key.encode("ascii") + rb"=([0-9]+)(?=\r?$)", original)
        if len(found) != 1 or int(found[0]) != int(number):
            raise RuntimeError("unchanged integer precondition failed: " + key)
        preserved[key] = int(found[0])
    pattern = rb"(?m)^" + args.key.encode("ascii") + rb"=([0-9]+)(?=\r?$)"
    matches = list(re.finditer(pattern, original))
    if len(matches) != 1:
        raise RuntimeError("expected exactly one integer assignment")
    match = matches[0]
    value = int(match.group(1)) + args.amount
    if value < 0:
        raise RuntimeError("result must remain a nonnegative integer")
    expected = original[:match.start(1)] + str(value).encode("ascii") + original[match.end(1):]
    plan = {"version": 1, "files": [{"path": args.path, "sha256": file["sha256"], "content": expected.decode("utf-8")}]}
    # preimage와 실제 쓰기·원문 및 권한 재검증은 기존 Go apply에 맡긴다.
    written = call(["apply", "--root", args.root, "--plan", "-", "--apply"], plan)
    output = written["files"][0]
    after_sha = hashlib.sha256(expected).hexdigest()
    if not output["verified"] or output["sha256"] != after_sha:
        raise RuntimeError("final source SHA mismatch")
    print(json.dumps({"path": args.path, "before_sha256": file["sha256"], "after_sha256": after_sha, "key": args.key, "value": value, "preserved_integers": preserved, "verified": True, "unrelated_preserved": True}, ensure_ascii=False))


if __name__ == "__main__":
    main()
