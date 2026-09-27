from fake_store import FakeStore
import asyncio
import unittest

import httpx

from job_manager import JobManager, _seven_day_usage
from sub2api_client import AdminAPIError, Sub2APIClient
from test_web_service import FakeClient


class UsageWindowTests(unittest.IsolatedAsyncioTestCase):
    def test_usage_preserves_zero_and_full_windows(self):
        for used in (0, 34.5, 100):
            usage = _seven_day_usage({"seven_day": {"utilization": used}})
            self.assertEqual(usage["used_percent"], used)
            self.assertEqual(usage["remaining_percent"], 100 - used)

    def test_missing_or_invalid_usage_is_not_zero(self):
        for used in (None, "", "35", True, -1, 101, float("nan"), float("inf")):
            usage = _seven_day_usage({"seven_day": {"utilization": used}})
            self.assertIsNone(usage["used_percent"])
            self.assertIsNone(usage["remaining_percent"])
        self.assertIsNone(_seven_day_usage({})["used_percent"])

    def test_snapshot_times_are_normalized_and_secrets_excluded(self):
        usage = _seven_day_usage({
            "updated_at": "2026-09-27T12:00:00Z",
            "private_key": "do-not-expose",
            "seven_day": {"utilization": 42, "resets_at": "2026-10-01T12:00:00Z"},
        })
        self.assertEqual(usage, {
            "used_percent": 42, "remaining_percent": 58,
            "reset_at": "2026-10-01T12:00:00+00:00",
            "updated_at": "2026-09-27T12:00:00+00:00",
            "account_cost": None, "estimated_total_cost": None,
        })
        self.assertIsNone(_seven_day_usage({"seven_day": {"resets_at": "invalid"}})["reset_at"])

    async def test_monitor_updates_usage_and_clears_missing_snapshot(self):
        client = FakeClient()
        client.reference["extra"]["codex_7d_used_percent"] = 38
        manager = JobManager(client, store=FakeStore(), watch_seconds=2, poll_seconds=0.01)
        try:
            watched = await manager.watch_reference(285)
            job = manager.get_job(watched["id"])
            for _ in range(50):
                if job.seven_day_usage is not None:
                    break
                await asyncio.sleep(0.01)
            self.assertEqual(job.public()["seven_day_usage"]["used_percent"], 38)
            client.reference["extra"].pop("codex_7d_used_percent")
            for _ in range(50):
                if job.seven_day_usage["used_percent"] is None:
                    break
                await asyncio.sleep(0.01)
            self.assertIsNone(job.public()["seven_day_usage"]["used_percent"])
        finally:
            await manager.close()

    def test_account_billing_uses_account_cost_and_same_window_percent(self):
        usage = _seven_day_usage({"seven_day": {
            "utilization": 25, "resets_at": "2099-01-01T00:00:00Z",
            "window_stats": {"cost": 12.5, "standard_cost": 50, "user_cost": 80},
        }})
        self.assertEqual(usage["account_cost"], 12.5)
        self.assertEqual(usage["estimated_total_cost"], 50)

    def test_missing_zero_invalid_cost_or_expired_window_is_not_estimated(self):
        for cost in (None, 0, -1, True, "12", float("nan"), float("inf")):
            with self.subTest(cost=cost):
                usage = _seven_day_usage({"seven_day": {"utilization": 25, "window_stats": {"cost": cost}}})
                self.assertIsNone(usage["estimated_total_cost"])
                if cost == 0:
                    self.assertEqual(usage["account_cost"], 0)
        for percent in (None, 0, -1, True, 101):
            usage = _seven_day_usage({"seven_day": {"utilization": percent, "window_stats": {"cost": 10}}})
            self.assertIsNone(usage["estimated_total_cost"])
        usage = _seven_day_usage({"seven_day": {
            "utilization": 25, "window_stats": {"cost": 10}, "resets_at": "2020-01-01T00:00:00Z",
        }})
        self.assertIsNone(usage["estimated_total_cost"])
        self.assertEqual(usage["account_cost"], 10)

    async def test_usage_failure_clears_financial_values_without_changing_auth_status(self):
        client = FakeClient()
        async def unavailable(account_id):
            raise AdminAPIError("private upstream error")
        client.get_usage = unavailable
        manager = JobManager(client, store=FakeStore(), watch_seconds=2, poll_seconds=0.01)
        try:
            watched = await manager.watch_reference(285)
            job = manager.get_job(watched["id"])
            job.seven_day_usage = {"account_cost": 12, "estimated_total_cost": 48}
            for _ in range(50):
                if "error" in job.seven_day_usage:
                    break
                await asyncio.sleep(0.01)
            self.assertEqual(job.state, "monitoring")
            self.assertEqual(job.seven_day_usage, {"error": "用量与计费查询失败，等待重试"})
        finally:
            await manager.close()

    async def test_client_reads_existing_usage_endpoint_without_force(self):
        def respond(request):
            self.assertEqual(request.method, "GET")
            self.assertEqual(request.url.path, "/api/v1/admin/accounts/292/usage")
            self.assertFalse(request.url.query)
            return httpx.Response(200, json={"code": 0, "data": {"seven_day": {"window_stats": {"cost": 12}}}})
        client = Sub2APIClient("https://sub2api.example", "test-key", httpx.MockTransport(respond))
        try:
            data = await client.get_usage(292)
            self.assertEqual(data["seven_day"]["window_stats"]["cost"], 12)
        finally:
            await client.close()
