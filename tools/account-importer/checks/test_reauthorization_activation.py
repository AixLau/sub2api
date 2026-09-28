import json
import unittest
from copy import deepcopy
from unittest.mock import AsyncMock, Mock

import httpx

from fake_store import FakeStore
from job_manager import Job, JobManager
from sub2api_client import AdminAPIError, Sub2APIClient
from test_web_service import bundle

LINE = "account@example.com----test-password----JBSWY3DPEHPK3PXP"


class ReauthorizationActivationTests(unittest.IsolatedAsyncioTestCase):
    async def exercise(self, path, status, failure=None):
        record = {
            **bundle()["accounts"][0], "id": 1, "status": status,
            "schedulable": False, "error_message": "401" if status == "error" else "",
            "concurrency": 30, "priority": 1, "proxy_id": None,
        }
        before = deepcopy(record)
        requests = []

        def respond(request):
            endpoint = request.url.path.removeprefix("/api/v1/admin/")
            body = json.loads(request.content) if request.content else None
            requests.append((request.method, endpoint, body))
            if endpoint == "proxies/all":
                data = []
            elif endpoint == "openai/refresh-token":
                data = {"email": record["name"], "access_token": "fresh-token"}
            elif endpoint.endswith("/apply-oauth-credentials"):
                if failure == "oauth":
                    return httpx.Response(400, json={"code": 400})
                record["credentials"].update(body["credentials"])
                record["error_message"] = ""
                # Deliberately leave the disabled/error state unchanged.
                data = record
            elif request.method == "PUT" and endpoint == "accounts/1":
                if failure == "status":
                    return httpx.Response(500, json={"code": 500})
                if failure != "status_not_saved":
                    record.update(body)
                data = record
            elif endpoint == "accounts/1/schedulable":
                if failure == "scheduling":
                    return httpx.Response(500, json={"code": 500})
                if failure != "scheduling_not_saved":
                    record.update(body)
                data = record
            elif request.method == "GET" and endpoint == "accounts/1":
                data = record
            else:
                raise AssertionError((request.method, endpoint))
            return httpx.Response(200, json={"code": 0, "data": data})

        client = Sub2APIClient("https://example.test", "test-key", httpx.MockTransport(respond))
        manager = JobManager(client, store=FakeStore(), login=lambda *a, **k: bundle(token="fresh-token"))
        manager._start_watch = AsyncMock()
        manager._resume_watch = Mock()
        job = Job(name=record["name"], mode="file" if path in {"rt", "json"} else "credentials",
                  account_id=1, group_ids=[2], credential_line=LINE, state="invalid")
        manager.jobs[job.id] = job
        try:
            if path == "automatic":
                await manager._retry_relogin(job)
            elif path == "manual":
                await manager.reauthorize_credentials(job.id)
                await job.task
            elif path == "rt":
                await manager.reauthorize_rt(job.id, "refresh-one")
                await job.task
            elif path == "json":
                await manager.reauthorize_file(job.id, bundle(token="fresh-token"))
            else:
                with self.assertRaises(AdminAPIError):
                    await client.apply_oauth(1, bundle()["accounts"][0])
            if failure:
                if path != "client":
                    self.assertEqual(job.state, "invalid")
                manager._start_watch.assert_not_awaited()
                if failure == "oauth":
                    self.assertEqual(record, before)
                    self.assertFalse(any(method == "PUT" or endpoint.endswith("/schedulable")
                                         for method, endpoint, _ in requests))
            else:
                self.assertEqual(record["status"], "active")
                self.assertIs(record["schedulable"], True)
                self.assertEqual(record["error_message"], "")
                for key in ("id", "name", "group_ids", "concurrency", "priority"):
                    self.assertEqual(record[key], before[key])
                self.assertEqual(record["credentials"]["model_mapping"], before["credentials"]["model_mapping"])
                self.assertEqual(record["credentials"]["access_token"], "fresh-token")
                oauth = next(i for i, (_, endpoint, _) in enumerate(requests) if endpoint.endswith("/apply-oauth-credentials"))
                self.assertEqual(requests[oauth + 1:oauth + 4], [
                    ("PUT", "accounts/1", {"status": "active"}),
                    ("POST", "accounts/1/schedulable", {"schedulable": True}),
                    ("GET", "accounts/1", None),
                ])
                if path != "automatic":
                    manager._start_watch.assert_awaited_once_with(job)
        finally:
            await manager.close()

    async def test_every_authorization_path_enables_disabled_and_error_accounts(self):
        for path in ("automatic", "manual", "rt", "json"):
            for status in ("disabled", "error"):
                with self.subTest(path=path, status=status):
                    await self.exercise(path, status)

    async def test_failed_authorization_does_not_enable_accounts(self):
        for path in ("automatic", "manual", "rt", "json"):
            with self.subTest(path=path):
                await self.exercise(path, "disabled", "oauth")

    async def test_failed_or_unpersisted_activation_is_not_success(self):
        for failure in ("status", "scheduling", "status_not_saved", "scheduling_not_saved"):
            with self.subTest(failure=failure):
                await self.exercise("client", "error", failure)

    async def test_activation_failure_does_not_restart_successful_watch(self):
        for path in ("automatic", "manual", "rt", "json"):
            with self.subTest(path=path):
                await self.exercise(path, "disabled", "scheduling")
