import json
import tempfile
import unittest
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import codex_usage_recover as recover


class CodexUsageRecoverTest(unittest.TestCase):
    def test_aggregates_token_delta_and_applies_multiplier_cutoff(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            session_dir = root / ".codex" / "sessions" / "2026" / "06" / "08"
            session_dir.mkdir(parents=True)
            session_file = session_dir / "rollout-2026-06-08T00-00-00-test.jsonl"
            session_file.write_text(
                "\n".join(
                    [
                        json.dumps(
                            {
                                "timestamp": "2026-06-07T15:59:00.000Z",
                                "type": "session_meta",
                                "payload": {
                                    "id": "s1",
                                    "model_provider": "custom",
                                    "cwd": "/repo",
                                },
                            }
                        ),
                        json.dumps(
                            {
                                "timestamp": "2026-06-07T15:59:30.000Z",
                                "type": "turn_context",
                                "payload": {"model": "gpt-test"},
                            }
                        ),
                        json.dumps(
                            {
                                "timestamp": "2026-06-07T15:59:59.000Z",
                                "type": "event_msg",
                                "payload": {
                                    "type": "token_count",
                                    "info": {
                                        "total_token_usage": {
                                            "input_tokens": 100,
                                            "cached_input_tokens": 40,
                                            "output_tokens": 10,
                                            "reasoning_output_tokens": 3,
                                            "total_tokens": 110,
                                        }
                                    },
                                },
                            }
                        ),
                        json.dumps(
                            {
                                "timestamp": "2026-06-07T16:00:01.000Z",
                                "type": "event_msg",
                                "payload": {
                                    "type": "token_count",
                                    "info": {
                                        "total_token_usage": {
                                            "input_tokens": 180,
                                            "cached_input_tokens": 60,
                                            "output_tokens": 20,
                                            "reasoning_output_tokens": 4,
                                            "total_tokens": 200,
                                        }
                                    },
                                },
                            }
                        ),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            (root / ".codex" / "config.toml").write_text(
                'model = "gpt-test"\nmodel_provider = "custom"\n'
                '[model_providers.custom]\nbase_url = "https://aixlau.me"\n',
                encoding="utf-8",
            )
            price_file = root / "prices.json"
            price_file.write_text(
                json.dumps(
                    {
                        "gpt-test": {
                            "input_cost_per_token": 0.001,
                            "cache_read_input_token_cost": 0.0001,
                            "output_cost_per_token": 0.01,
                        }
                    }
                ),
                encoding="utf-8",
            )

            result = recover.run_report(
                codex_home=root / ".codex",
                price_file=price_file,
                supplier_url="https://aixlau.me",
                multiplier_from="2026-06-08",
                multiplier=1.3,
                timezone_name="Asia/Shanghai",
                since=None,
                until=None,
                include_unknown_supplier=False,
            )

        self.assertEqual(result.summary["requests"], 2)
        self.assertEqual(result.summary["input_tokens"], 180)
        self.assertEqual(result.summary["cached_input_tokens"], 60)
        self.assertAlmostEqual(result.summary["base_cost"], 0.326)
        self.assertAlmostEqual(result.summary["final_cost"], 0.3746)
        self.assertEqual(result.summary["supplier_match"], "direct")

    def test_unknown_model_is_kept_with_zero_cost(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            session_dir = root / ".codex" / "sessions" / "2026" / "06" / "09"
            session_dir.mkdir(parents=True)
            (session_dir / "rollout-test.jsonl").write_text(
                "\n".join(
                    [
                        json.dumps(
                            {
                                "timestamp": "2026-06-09T00:00:00.000Z",
                                "type": "session_meta",
                                "payload": {"id": "s2"},
                            }
                        ),
                        json.dumps(
                            {
                                "timestamp": "2026-06-09T00:00:01.000Z",
                                "type": "event_msg",
                                "payload": {
                                    "type": "token_count",
                                    "info": {
                                        "total_token_usage": {
                                            "input_tokens": 10,
                                            "cached_input_tokens": 0,
                                            "output_tokens": 5,
                                        }
                                    },
                                },
                            }
                        ),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            (root / ".codex" / "config.toml").write_text(
                '[model_providers.custom]\nbase_url = "https://aixlau.me"\n',
                encoding="utf-8",
            )
            price_file = root / "prices.json"
            price_file.write_text("{}", encoding="utf-8")

            result = recover.run_report(
                codex_home=root / ".codex",
                price_file=price_file,
                supplier_url="https://aixlau.me",
                multiplier_from="2026-06-08",
                multiplier=1.3,
                timezone_name="Asia/Shanghai",
                since=None,
                until=None,
                include_unknown_supplier=False,
            )

        self.assertEqual(result.summary["requests"], 1)
        self.assertEqual(result.summary["final_cost"], 0.0)
        self.assertEqual(result.summary["unknown_model_requests"], 1)


if __name__ == "__main__":
    unittest.main()
