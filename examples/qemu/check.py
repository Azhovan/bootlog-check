#!/usr/bin/env python3
"""Independent oracle for this fixed example, not a general rule evaluator."""
import argparse
import json
from pathlib import Path
import re
import subprocess

HERE = Path(__file__).resolve().parent
CASES = ("normal-1", "normal-2", "missing", "forbidden", "delayed-1", "delayed-2")
SCOPE = "Results describe only the supplied capture; a pass does not prove capture completeness or device health."
RULES = {
    "schema_version": 1,
    "boot_start": {"pattern": r"^Linux version 6\.12\.60-bootlog "},
    "milestones": [{"id": "ready", "pattern": "^BOOTLOG_READY$", "within_ms": 5000}],
    "forbidden": [{"id": "fault", "pattern": "^BOOTLOG_FORBIDDEN$"}],
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def strict_json(data):
    def object_pairs(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "duplicate JSON key")
            result[key] = value
        return result

    def reject_number(value):
        raise ValueError(f"non-integer JSON number: {value}")

    result = json.loads(data, object_pairs_hook=object_pairs,
                        parse_float=reject_number, parse_constant=reject_number)

    def no_bools(value):
        require(type(value) is not bool, "unexpected JSON boolean")
        if isinstance(value, dict):
            for item in value.values():
                no_bools(item)
        elif isinstance(value, list):
            for item in value:
                no_bools(item)
    no_bools(result)
    return result


def expected_report(path, scenario):
    """Read every raw physical line and compute integer nanoseconds independently."""
    require(scenario in ("normal", "missing", "forbidden", "delayed"), "bad scenario")
    raw = path.read_bytes()
    require(raw.endswith(b"\n"), "incomplete serial line")
    records = []
    previous = -1
    for number, line in enumerate(raw.split(b"\n")[:-1], 1):
        if line.endswith(b"\r"):
            line = line[:-1]
        text = line.decode("utf-8", errors="strict")
        match = re.fullmatch(r"\[[ \t]*([0-9]+)\.([0-9]{1,9})\] ?(.*)", text)
        require(match is not None, f"line {number}: unexpected serial grammar")
        timestamp = int(match[1]) * 1_000_000_000 + int(match[2].ljust(9, "0"))
        require(previous <= timestamp <= 2**63 - 1, f"line {number}: invalid clock")
        previous = timestamp
        records.append({"line": number, "timestamp_ns": timestamp, "message": match[3]})
    require(records, "empty capture")
    require(not any("BOOTLOG_GUEST_ERROR" in r["message"] for r in records), "guest error")

    def exact(message):
        return [r for r in records if r["message"] == message]

    starts = [r for r in records if r["message"].startswith("Linux version ")]
    require(len(starts) == 1 and starts[0]["message"].startswith("Linux version 6.12.60-bootlog "),
            "missing, duplicate, or wrong Linux origin")
    origin = starts[0]
    names = [r for r in records if r["message"].startswith("BOOTLOG_SCENARIO ")]
    require(len(names) == 1 and names[0]["message"] == f"BOOTLOG_SCENARIO {scenario}",
            "scenario provenance mismatch")
    ready, fault = exact("BOOTLOG_READY"), exact("BOOTLOG_FORBIDDEN")
    complete, poweroff = exact("BOOTLOG_COMPLETE"), exact("reboot: Power down")
    require(len(complete) == len(poweroff) == 1, "capture has no unique completion/poweroff")
    require(origin["line"] < names[0]["line"] < complete[0]["line"] < poweroff[0]["line"],
            "invalid guest completion order")
    require(poweroff[0] == records[-1], "unexpected output after poweroff")
    require(len(ready) == (0 if scenario == "missing" else 1), "wrong ready marker count")
    require(len(fault) == (1 if scenario == "forbidden" else 0), "wrong fault marker count")
    for witness in ready + fault:
        require(names[0]["line"] < witness["line"] < complete[0]["line"], "wrong marker order")
    if fault:
        require(ready[0]["line"] < fault[0]["line"], "fault must follow readiness")

    elapsed = ready[0]["timestamp_ns"] - origin["timestamp_ns"] if ready else None
    if elapsed is not None:
        if scenario == "delayed":
            require(elapsed >= 10_000_000_000, "guest delay did not reach ten seconds")
        else:
            require(1_000_000_000 <= elapsed <= 5_000_000_000, "normal timing outside margin")

    def check(identifier, kind, matches, status, reason, deadline=None):
        evidence = matches[0] if matches else None
        return {"id": identifier, "kind": kind, "status": status, "reason": reason,
                "within_ms": deadline,
                "elapsed_ns": evidence["timestamp_ns"] - origin["timestamp_ns"] if evidence else None,
                "match_count": len(matches), "evidence": evidence}

    checks = [check("boot_start", "start", starts, "passed", "matched"),
              check("ready", "milestone", ready,
                    "failed" if scenario in ("missing", "delayed") else "passed",
                    {"missing": "missing", "delayed": "late"}.get(scenario, "matched"), 5000),
              check("fault", "forbidden", fault, "failed" if fault else "passed",
                    "forbidden_match" if fault else "absent")]
    failed = 0 if scenario == "normal" else 1
    return {"schema_version": 1, "timing_basis": "boot_start", "origin": origin,
            "checks": checks, "summary": {"total": 3, "passed": 3 - failed, "failed": failed,
                                         "not_evaluated": 0}, "scope": SCOPE}


def replay(cli, captures, write=False):
    require(strict_json((HERE / "rules.json").read_bytes()) == RULES, "example rules changed")
    for case in CASES:
        path = captures / f"{case}.log"
        expected = expected_report(path, case.split("-")[0])
        expected_exit = 0 if case.startswith("normal") else 1
        outputs = []
        for _ in range(2):
            run = subprocess.run([str(cli), "--rules", str(HERE / "rules.json"),
                                  "--format", "json", str(path)], capture_output=True, timeout=15)
            require(run.returncode == expected_exit, f"{case}: CLI exit {run.returncode}")
            require(not run.stderr, f"{case}: unexpected stderr {run.stderr!r}")
            require(strict_json(run.stdout) == expected, f"{case}: report differs from raw evidence")
            outputs.append(run.stdout)
        require(outputs[0] == outputs[1], f"{case}: nondeterministic replay")
        report_path = captures / f"{case}.json"
        if write:
            require(not report_path.exists(), f"refusing to replace {report_path}")
            report_path.write_bytes(outputs[0])
        else:
            require(report_path.read_bytes() == outputs[0], f"{case}: saved report differs")
        witness = expected["checks"][1]
        print(f"{case}: exit={expected_exit} ready={witness['reason']} "
              f"elapsed_ns={witness['elapsed_ns']} deadline_ns=5000000000")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cli", type=Path, required=True)
    parser.add_argument("--captures", type=Path, required=True)
    parser.add_argument("--write-reports", action="store_true")
    args = parser.parse_args()
    replay(args.cli.resolve(), args.captures.resolve(), args.write_reports)
