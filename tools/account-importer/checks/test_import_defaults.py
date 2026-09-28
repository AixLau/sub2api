import asyncio
from copy import deepcopy
import unittest
from unittest.mock import patch

import httpx

from fake_store import FakeStore, wait_for_operation
from job_manager import JobManager, _account_profile
from proxy_pool import select_5x_proxy
from sub2api_client import Sub2APIClient
from test_web_service import FakeClient, bundle

LINE = "account@example.com----password----JBSWY3DPEHPK3PXP"


def proxy(proxy_id, **overrides):
    return {"id": proxy_id, "name": "5x", "status": "active", "protocol": "http",
            "host": "proxy.example", "port": 8000 + proxy_id, **overrides}


class ImportClient(FakeClient):
    def __init__(self):
        super().__init__()
        self.reference.update(status="disabled", schedulable=False, proxy_id=999)
        self.proxies = [proxy(1), proxy(2), proxy(999, name="reference-only")]
        self.payloads = []
        self.proxy_reads = 0

    async def list_active_proxies(self):
        self.proxy_reads += 1
        return deepcopy(self.proxies)

    async def import_data(self, payload):
        self.payloads.append(deepcopy(payload))
        return await super().import_data(payload)


class ImportDefaultTests(unittest.IsolatedAsyncioTestCase):
    async def test_new_import_modes_ignore_reference_state_and_proxy(self):
        for mode in ("file", "credentials"):
            for state in ("disabled", "error"):
                with self.subTest(mode=mode, state=state):
                    client = ImportClient()
                    client.reference["status"] = state
                    document = bundle()
                    document["proxies"] = [{"proxy_key": "uploaded-old-proxy"}]
                    document["accounts"][0].update(status="disabled", schedulable=False,
                                                   proxy_id=777, proxy_key="uploaded-old-proxy")
                    original = deepcopy(document)
                    logins = []
                    def login(*args, **kwargs):
                        logins.append(kwargs["proxy"])
                        return deepcopy(document)
                    manager = JobManager(client, store=FakeStore(), login=login)
                    try:
                        with patch("proxy_pool.secrets.choice", side_effect=lambda choices: choices[-1]):
                            if mode == "file":
                                public = (await manager.import_file(document, profile_account_id=285))[0]
                            else:
                                public = await manager.import_credentials(LINE, profile_account_id=285)
                                await wait_for_operation(manager.get_job(public["id"]))
                        job = manager.get_job(public["id"])
                        self.assertEqual(job.state, "monitoring")
                        saved = client.records["account@example.com"]
                        self.assertEqual(saved["status"], "active")
                        self.assertIs(saved["schedulable"], True)
                        self.assertEqual(saved["proxy_id"], 2)
                        self.assertEqual(saved["group_ids"], [2, 15])
                        self.assertEqual(saved["concurrency"], client.reference["concurrency"])
                        sent = client.payloads[0]
                        self.assertEqual(sent["proxies"], [])
                        self.assertEqual(sent["accounts"][0]["proxy_key"], "http|proxy.example|8002||")
                        self.assertEqual(document, original)
                        if mode == "credentials":
                            self.assertEqual(logins, ["http://proxy.example:8002"])
                        self.assertEqual(client.reference["status"], state)
                        self.assertIs(client.reference["schedulable"], False)
                    finally:
                        await manager.close()

    async def test_batch_selects_independently_from_complete_pool(self):
        client = ImportClient()
        client.proxies = [proxy(i) for i in range(1, 121)]
        document = bundle("first@example.com")
        document["accounts"].extend(bundle("second@example.com")["accounts"])
        picks = iter((119, 0))
        def choose(candidates):
            self.assertEqual(len(candidates), 120)
            return candidates[next(picks)]
        manager = JobManager(client, store=FakeStore())
        try:
            with patch("proxy_pool.secrets.choice", side_effect=choose):
                jobs = await manager.import_file(document, [42], 285)
            self.assertEqual([job["proxy_id"] for job in jobs], [120, 1])
            self.assertEqual([job["group_ids"] for job in jobs], [[42], [42]])
            self.assertEqual(client.proxy_reads, 1)
        finally:
            await manager.close()

    async def test_empty_pool_imports_directly_in_both_modes(self):
        for mode in ("file", "credentials"):
            for proxies in ([], [proxy(1, name="other"), proxy(2, status="disabled"),
                                proxy(3, expires_at="2000-01-01T00:00:00Z")]):
                with self.subTest(mode=mode, proxies=proxies):
                    client, calls = ImportClient(), []
                    client.proxies = proxies
                    document = bundle()
                    document["accounts"][0].update(proxy_id=999, proxy_key="uploaded-old-proxy")
                    def login(*args, **kwargs):
                        calls.append(kwargs["proxy"])
                        return deepcopy(document)
                    manager = JobManager(client, store=FakeStore(), login=login)
                    try:
                        if mode == "file":
                            public = (await manager.import_file(document, profile_account_id=285))[0]
                        else:
                            public = await manager.import_credentials(LINE, profile_account_id=285)
                            await wait_for_operation(manager.get_job(public["id"]))
                        job = manager.get_job(public["id"])
                        self.assertEqual(job.state, "monitoring")
                        self.assertIsNone(job.proxy_id)
                        self.assertIsNone(job.login_proxy)
                        sent = client.payloads[0]
                        self.assertEqual(sent["proxies"], [])
                        self.assertNotIn("proxy_id", sent["accounts"][0])
                        self.assertNotIn("proxy_key", sent["accounts"][0])
                        saved = client.records["account@example.com"]
                        self.assertEqual(saved["proxy_id"], 0)
                        self.assertEqual(saved["status"], "active")
                        self.assertIs(saved["schedulable"], True)
                        self.assertEqual(saved["credentials"]["model_mapping"], client.reference["credentials"]["model_mapping"])
                        self.assertEqual(calls, [None] if mode == "credentials" else [])
                    finally:
                        await manager.close()

    async def test_direct_reimport_clears_existing_proxy_in_both_modes(self):
        for mode in ("file", "credentials"):
            client, calls = ImportClient(), []
            client.proxies = []
            document = bundle()
            document["accounts"][0]["proxy_id"] = 999
            await client.import_data(document)
            def login(*args, **kwargs):
                calls.append(kwargs["proxy"])
                return bundle()
            manager = JobManager(client, store=FakeStore(), login=login)
            try:
                if mode == "file":
                    public = (await manager.import_file(bundle(), profile_account_id=285))[0]
                else:
                    public = await manager.import_credentials(LINE, profile_account_id=285)
                    await wait_for_operation(manager.get_job(public["id"]))
                self.assertEqual(client.records["account@example.com"]["proxy_id"], 0)
                self.assertIsNone(manager.get_job(public["id"]).proxy_id)
                self.assertEqual(calls, [None] if mode == "credentials" else [])
                self.assertEqual(len(client.records), 1)
            finally:
                await manager.close()

    async def test_proxy_api_failure_is_not_treated_as_empty_pool(self):
        from sub2api_client import AdminAPIError
        for mode in ("file", "credentials"):
            client, calls = ImportClient(), []
            async def fail():
                raise AdminAPIError("proxy API unavailable", status_code=503)
            client.list_active_proxies = fail
            manager = JobManager(client, store=FakeStore(), login=lambda *a, **kw: calls.append(1))
            try:
                with self.assertRaises(AdminAPIError):
                    if mode == "file":
                        await manager.import_file(bundle(), profile_account_id=285)
                    else:
                        await manager.import_credentials(LINE, profile_account_id=285)
                self.assertFalse(client.payloads)
                self.assertFalse(manager.jobs)
                self.assertFalse(calls)
            finally:
                await manager.close()

    async def test_reimport_enables_existing_account_in_both_modes(self):
        for mode in ("file", "credentials"):
            client = ImportClient()
            document = bundle()
            document["accounts"][0].update(status="disabled", schedulable=False, proxy_id=999)
            await client.import_data(document)
            manager = JobManager(client, store=FakeStore(), login=lambda *a, **kw: bundle())
            try:
                if mode == "file":
                    await manager.import_file(bundle())
                else:
                    public = await manager.import_credentials(LINE)
                    await wait_for_operation(manager.get_job(public["id"]))
                record = client.records["account@example.com"]
                self.assertEqual(record["status"], "active")
                self.assertIs(record["schedulable"], True)
                self.assertIn(record["proxy_id"], [1, 2])
                self.assertEqual(len(client.records), 1)
            finally:
                await manager.close()

    async def test_proxy_api_uses_unpaginated_full_list(self):
        def respond(request):
            self.assertEqual(request.url.path, "/api/v1/admin/proxies/all")
            self.assertFalse(request.url.query)
            return httpx.Response(200, json={"code": 0, "data": [proxy(i) for i in range(1, 121)]})
        client = Sub2APIClient("https://example.test", "test", httpx.MockTransport(respond))
        try:
            self.assertEqual(len(await client.list_active_proxies()), 120)
        finally:
            await client.close()

    def test_reference_profile_excludes_state_and_proxy(self):
        profile = _account_profile(ImportClient().reference)
        self.assertNotIn("status", profile["settings"])
        self.assertNotIn("proxy_id", profile["settings"])
        self.assertNotIn("schedulable", profile)

    def test_proxy_pool_excludes_unusable_and_different_names(self):
        invalid = [proxy(3, status="disabled"), proxy(4, name="other"),
                   proxy(5, expires_at="2000-01-01T00:00:00Z"), proxy(6, protocol="ftp"),
                   proxy(7, port=0), proxy(8, username="user"), proxy(9, expires_at="bad")]
        with patch("proxy_pool.secrets.choice", side_effect=lambda candidates: candidates[-1]) as choose:
            selected = select_5x_proxy([proxy(1), *invalid, proxy(2)])
        self.assertEqual([p["id"] for p in choose.call_args.args[0]], [1, 2])
        self.assertEqual(selected.id, 2)

    def test_proxy_key_matches_backend_and_hides_secrets_in_repr(self):
        selected = select_5x_proxy([proxy(1, username="user", password="secret/password")])
        self.assertEqual(selected.key, "http|proxy.example|8001|user|secret/password")
        self.assertEqual(selected.url, "http://user:secret%2Fpassword@proxy.example:8001")
        self.assertNotIn("secret", repr(selected))


if __name__ == "__main__":
    unittest.main()
