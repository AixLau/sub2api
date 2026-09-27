from fake_store import FakeStore, wait_for_operation
import asyncio
import json
import re
import unittest
from datetime import timedelta

import httpx

from job_manager import Job, JobManager, _now
from sub2api_client import Sub2APIClient
from test_web_service import FakeClient, bundle
from web_service import create_app

LINE = "account@example.com----test-password----JBSWY3DPEHPK3PXP"


class ManualReauthorizationTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.client = FakeClient()
        await self.client.import_data(bundle())
        self.lines = []
        def login(line, fmt, **kwargs):
            self.lines.append(line)
            return bundle(token="manual-access")
        self.manager = JobManager(self.client, store=FakeStore(), login=login, poll_seconds=1)

    async def asyncTearDown(self):
        await self.manager.close()

    def job(self, mode="credentials", **kwargs):
        job = Job(name="account@example.com", mode=mode, group_ids=[2], account_id=1, state="completed", **kwargs)
        self.manager.jobs[job.id] = job
        return job

    async def test_saved_credentials_survive_watch_cancellation_and_reset_retry_budget(self):
        job = self.job(credential_line=LINE)
        await self.manager._start_watch(job)
        await asyncio.sleep(0)
        job.attempts = 3
        job.next_relogin = _now() + timedelta(hours=1)
        result = await self.manager.reauthorize_credentials(job.id)
        self.assertEqual(result["state"], "reauthorizing")
        task = job.task
        with self.assertRaisesRegex(ValueError, "正在授权"):
            await self.manager.reauthorize_credentials(job.id)
        await task
        self.assertEqual(self.lines, [LINE])
        self.assertEqual(self.client.applied, [(1, "manual-access")])
        self.assertEqual(job.attempts, 0)
        self.assertTrue(job.public()["monitoring_enabled"])
        self.assertEqual(job.credential_line, LINE)
        self.assertEqual(job.state, "monitoring")
        self.assertEqual(len(self.client.records), 1)
        self.assertNotIn("test-password", json.dumps(self.manager.list_jobs()))

    async def test_continuous_monitor_keeps_original_credentials_for_one_click(self):
        job = self.job(credential_line=LINE)
        await self.manager._start_watch(job)
        await asyncio.sleep(0.01)
        self.assertFalse(job.task.done())
        self.assertEqual(job.credential_line, LINE)
        await self.manager.reauthorize_credentials(job.id)
        await wait_for_operation(job)
        self.assertEqual(self.lines, [LINE])
        self.assertEqual(job.state, "monitoring")

    async def test_missing_original_credentials_rejected_without_login(self):
        job = self.job()
        with self.assertRaisesRegex(ValueError, "首次导入"):
            await self.manager.reauthorize_credentials(job.id)
        self.assertEqual(self.lines, [])

    async def test_busy_retired_and_wrong_mode_rejected(self):
        job = self.job(credential_line=LINE)
        with self.assertRaises(ValueError):
            await self.manager.reauthorize_rt(job.id, "secret-rt")
        job.state = "reauthorizing"
        with self.assertRaisesRegex(ValueError, "正在授权"):
            await self.manager.reauthorize_credentials(job.id)
        job.state, job.retired = "completed", True
        with self.assertRaises(ValueError):
            await self.manager.reauthorize_credentials(job.id)
        self.assertFalse(self.client.applied)

    async def test_failed_login_keeps_original_credentials_for_retry(self):
        job = self.job(credential_line=LINE)
        def fail(*args, **kwargs):
            raise RuntimeError("test-password must never be returned")
        self.manager.login = fail
        await self.manager.reauthorize_credentials(job.id)
        await wait_for_operation(job)
        self.assertEqual(job.state, "monitoring")
        self.assertFalse(job.task.done())
        self.assertEqual(job.credential_line, LINE)
        self.assertFalse(job.manual_pending)
        self.assertNotIn("test-password", json.dumps(job.public()))

    async def test_rt_task_success_failure_and_reupload_overlap(self):
        job = self.job("file")
        gate = asyncio.Event()
        calls = []
        async def refresh(account_id, token):
            calls.append((account_id, token))
            await gate.wait()
        self.client.refresh_oauth = refresh
        response = await self.manager.reauthorize_rt(job.id, "  secret-rt  ")
        task = job.task
        self.assertEqual(response["state"], "reauthorizing")
        with self.assertRaisesRegex(ValueError, "正在授权"):
            await self.manager.reauthorize_file(job.id, bundle())
        gate.set()
        await task
        self.assertEqual(calls, [(1, "secret-rt")])
        self.assertEqual(job.state, "monitoring")
        self.assertNotIn("secret-rt", json.dumps(job.public()))
        async def fail(*args):
            raise RuntimeError("secret-rt")
        self.client.refresh_oauth = fail
        await self.manager.reauthorize_rt(job.id, "secret-rt")
        await wait_for_operation(job)
        self.assertEqual(job.state, "monitoring")
        self.assertFalse(job.task.done())
        self.assertNotIn("secret-rt", job.message)
        self.assertFalse(job.manual_pending)

    async def test_api_csrf_validation_and_async_credentials(self):
        app = create_app(self.client, store=FakeStore(), login=self.manager.login)
        job = self.job(credential_line=LINE)
        app.state.manager.jobs[job.id] = job
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app), base_url="http://localhost") as http:
            page = await http.get("/")
            csrf = re.search(r'name="csrf-token" content="([^"]+)"', page.text).group(1)
            path = f"/api/jobs/{job.id}/reauthorize/credentials"
            self.assertEqual((await http.post(path, json={"account_line": LINE})).status_code, 403)
            headers = {"X-CSRF-Token": csrf}
            # No second credentials input; even a supplied body cannot replace the original.
            response = await http.post(path, headers=headers, json={"account_line": "other@example.com----replacement----JBSWY3DPEHPK3PXP"})
            self.assertEqual(response.status_code, 202, response.text)
            self.assertNotIn("test-password", response.text)
            rt_path = f"/api/jobs/{job.id}/reauthorize/refresh-token"
            self.assertEqual((await http.post(rt_path, json={"refresh_token": "secret"})).status_code, 403)
        await wait_for_operation(job)
        self.assertEqual(self.lines, [LINE])
        await app.state.manager.close()


    async def test_empty_pool_manual_credentials_and_json_reupload_clear_old_proxy(self):
        calls = []
        async def no_proxies():
            return []
        def login(line, fmt, **kwargs):
            calls.append((line, kwargs["proxy"]))
            return bundle(token="manual-direct")
        self.client.list_active_proxies = no_proxies
        self.manager.login = login
        for mode in ("credentials", "file"):
            job = self.job(mode, credential_line=LINE if mode == "credentials" else None,
                           proxy_id=999, login_proxy="http://old-proxy:8080")
            self.client.records["account@example.com"]["proxy_id"] = 999
            if mode == "credentials":
                await self.manager.reauthorize_credentials(job.id)
                await wait_for_operation(job)
                self.assertEqual(calls, [(LINE, None)])
                self.assertIsNone(job.login_proxy)
            else:
                await self.manager.reauthorize_file(job.id, bundle())
            self.assertEqual(self.client.records["account@example.com"]["proxy_id"], 0)
            self.assertIsNone(job.proxy_id)
            self.assertTrue(job.public()["monitoring_enabled"])


class RefreshClientTests(unittest.IsolatedAsyncioTestCase):
    async def exercise(self, info, expected_error=None, status=200):
        writes = []
        remote = {"id": 1, "platform": "openai", "type": "oauth", "name": "account@example.com", "proxy_id": 22,
                  "credentials": {"email": "account@example.com", "client_id": "existing-client", "model_mapping": {"x": "y"}},
                  "extra": {"setting": True}}
        def respond(request):
            if request.method == "GET":
                data = remote
            elif request.url.path.endswith("/refresh-token"):
                self.assertEqual(json.loads(request.content), {"refresh_token": "submitted-rt", "proxy_id": 22, "client_id": "existing-client"})
                return httpx.Response(status, json={"code": 0 if status == 200 else 400, "data": info})
            else:
                writes.append(json.loads(request.content))
                data = {}
            return httpx.Response(200, json={"code": 0, "data": data})
        client = Sub2APIClient("https://test.example", "test-key", httpx.MockTransport(respond))
        try:
            if expected_error:
                with self.assertRaises(expected_error):
                    await client.refresh_oauth(1, "submitted-rt")
                self.assertEqual(writes, [])
            else:
                await client.refresh_oauth(1, "submitted-rt")
                self.assertNotIn("model_mapping", writes[0]["credentials"])
                self.assertEqual(remote["credentials"]["model_mapping"], {"x": "y"})
                self.assertEqual(writes[0]["extra"], {"setting": True})
            return writes
        finally:
            await client.close()

    async def test_refresh_applies_rotated_token_and_preserves_settings(self):
        writes = await self.exercise({"access_token": "new-access", "refresh_token": "rotated-rt", "email": "ACCOUNT@example.com", "expires_at": 1900000000})
        self.assertEqual(writes[0]["credentials"]["refresh_token"], "rotated-rt")
        self.assertTrue(writes[0]["credentials"]["expires_at"].startswith("2030-"))

    async def test_non_rotating_token_retains_submitted_rt(self):
        writes = await self.exercise({"access_token": "new-access", "email": "account@example.com"})
        self.assertEqual(writes[0]["credentials"]["refresh_token"], "submitted-rt")

    async def test_wrong_or_unverifiable_identity_never_written(self):
        await self.exercise({"access_token": "new-access", "email": "other@example.com"}, ValueError)
        await self.exercise({"access_token": "new-access"}, ValueError)
        await self.exercise({"email": "account@example.com"}, ValueError)

    async def test_rejected_rt_never_written(self):
        from sub2api_client import AdminAPIError
        await self.exercise({}, AdminAPIError, status=400)
