import json
from pathlib import Path
import tempfile
import unittest

from compare_go_test_json import compare, counts, read_run


class RecordedGoTestEvidenceTest(unittest.TestCase):
    def parse(self, events):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "run.jsonl"
            path.write_text("\n".join(json.dumps(event) for event in events) + "\n")
            return read_run(path)[0]

    def event(self, action, test=None, **kwargs):
        event = {"Package": "example.test/service", "Action": action, **kwargs}
        if test is not None:
            event["Test"] = test
        return event

    def test_panic_does_not_convert_parallel_or_unobserved_test_to_pass(self):
        recorded = self.parse([
            self.event("start"), self.event("run", "TestParallel"),
            self.event("pause", "TestParallel"), self.event("run", "TestPanic"),
            self.event("output", "TestPanic", Output="panic: index out of range\n"),
            self.event("fail", "TestPanic"), self.event("fail"),
        ])
        self.assertEqual(recorded[("example.test/service", "TestParallel")]["status"], "INCOMPLETE")
        self.assertTrue(recorded[("example.test/service", "TestPanic")]["panicked"])
        self.assertNotIn(("example.test/service", "TestNeverStarted"), recorded)
        self.assertEqual(counts(recorded)["package"], {"FAIL": 1})

    def test_matching_failure_is_not_pass_and_subtests_remain_separate(self):
        run = self.parse([
            self.event("run", "TestParent"), self.event("run", "TestParent/child"),
            self.event("fail", "TestParent/child"), self.event("fail", "TestParent"),
        ])
        rows = compare(run, run)
        self.assertEqual(len(rows), 2)
        self.assertEqual({row["comparison"] for row in rows}, {"FAIL_TO_FAIL"})
        self.assertEqual({row["acceptance"] for row in rows}, {"NOT_PASS"})
        self.assertEqual({row["attribution"] for row in rows}, {"UNATTRIBUTED"})

    def test_candidate_only_failure_is_not_asserted_to_be_regression(self):
        baseline = self.parse([self.event("skip")])
        candidate = self.parse([self.event("run", "TestNew"), self.event("fail", "TestNew")])
        row = compare(baseline, candidate)[0]
        self.assertEqual(row["comparison"], "NOT_RUN_TO_FAIL")
        self.assertEqual(row["attribution"], "UNATTRIBUTED")

    def test_repeated_run_cannot_hide_earlier_failure(self):
        run = self.parse([self.event("run", "TestRetry"), self.event("fail", "TestRetry"),
                          self.event("run", "TestRetry"), self.event("pass", "TestRetry")])
        self.assertEqual(run[("example.test/service", "TestRetry")]["status"], "FAIL")

    def test_empty_log_is_not_success(self):
        with self.assertRaises(ValueError):
            self.parse([])

    def test_build_failure_has_package_evidence_without_fabricated_test(self):
        run = self.parse([self.event("output", Output="FAIL example.test/service [build failed]\n"),
                          self.event("fail")])
        self.assertEqual(counts(run)["package"], {"FAIL": 1})
        self.assertEqual(counts(run)["root_test"], {})


if __name__ == "__main__":
    unittest.main()
