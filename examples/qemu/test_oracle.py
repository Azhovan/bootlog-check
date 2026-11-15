#!/usr/bin/env python3
"""Offline negative checks using the saved real captures, never synthetic proof."""
import copy
import json
from pathlib import Path
import tempfile
import unittest

from check import CASES, expected_report, strict_json

CAPTURES = Path(__file__).resolve().parent / "captures"


class OracleTests(unittest.TestCase):
    def test_all_saved_reports_match_independent_raw_evidence(self):
        for case in CASES:
            with self.subTest(case=case):
                expected = expected_report(CAPTURES / f"{case}.log", case.split("-")[0])
                self.assertEqual(strict_json((CAPTURES / f"{case}.json").read_bytes()), expected)

    def test_rejects_incomplete_malformed_and_wrong_scenario_capture(self):
        raw = (CAPTURES / "normal-1.log").read_bytes()
        variants = [raw[:raw.index(b"BOOTLOG_COMPLETE")],
                    raw + b"[1.1234567890] malformed after readiness\n",
                    raw.replace(b"BOOTLOG_SCENARIO normal", b"BOOTLOG_SCENARIO missing"),
                    raw.replace(b"BOOTLOG_READY", b"BOOTLOG_GUEST_ERROR write kmsg"),
                    raw.replace(b"BOOTLOG_READY", b"BOOTLOG_READY\x85injected")]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "corrupted.log"
            for variant in variants:
                with self.subTest(variant=variant[-80:]):
                    path.write_bytes(variant)
                    with self.assertRaises((ValueError, UnicodeError)):
                        expected_report(path, "normal")

    def test_rejects_ambiguous_report_numbers_and_duplicate_keys(self):
        for data in ('{"line":true}', '{"line":1.0}', '{"line":NaN}', '{"line":1,"line":2}'):
            with self.subTest(data=data), self.assertRaises(ValueError):
                strict_json(data)

    def test_raw_evidence_detects_tampered_report_fields(self):
        expected = expected_report(CAPTURES / "normal-1.log", "normal")
        report = strict_json((CAPTURES / "normal-1.json").read_bytes())
        for field in ("line", "timestamp_ns", "message"):
            bad = copy.deepcopy(report)
            bad["checks"][1]["evidence"][field] = "tampered"
            self.assertNotEqual(strict_json(json.dumps(bad)), expected)
        for field in ("elapsed_ns", "match_count"):
            bad = copy.deepcopy(report)
            bad["checks"][1][field] += 1
            self.assertNotEqual(strict_json(json.dumps(bad)), expected)
        bad = copy.deepcopy(report)
        bad["summary"]["failed"] = 1
        self.assertNotEqual(strict_json(json.dumps(bad)), expected)


if __name__ == "__main__":
    unittest.main()
