from fake_store import FakeStore
import asyncio
import json
import re
import unittest

import httpx

from import_payload import parse_bundle
from job_manager import JobManager, _account_state
from web_service import create_app
from sub2api_client import Sub2APIClient
from proxy_pool import select_5x_proxy


def bundle(name="account@example.com", token="token-one"):
    return {
        "proxies": [],
        "accounts": [{
            "name": name, "platform": "openai", "type": "oauth",
            "credentials": {"access_token": token, "refresh_token": "refresh-one"},
            "extra": {"email": name}, "group_ids": [2],
        }],
    }


class FakeClient:
    base_url = "https://sub2api.example"

    def __init__(self):
        self.records = {}
        self.applied = []
        self.settings_updates = []
        self.closed = False
        self.reference = {
            "id": 285, "name": "reference@example.com", "platform": "openai", "type": "oauth",
            "status": "active", "schedulable": True, "proxy_id": 17382,
            "concurrency": 30, "priority": 1, "rate_multiplier": 1,
            "auto_pause_on_expired": True, "current_concurrency": 3,
            "last_used_at": "2026-09-26T12:00:00Z",
            "group_ids": [2, 15],
            "account_groups": [{"group_id": 15, "priority": 2}, {"group_id": 2, "priority": 1}],
            "extra": {"openai_long_context_billing_enabled": True, "email_key": "must-not-copy"},
            "credentials_status": {"has_access_token": True, "has_refresh_token": True},
        }

    async def find_account(self, name):
        if name.casefold() == self.reference["name"]:
            return self.reference.copy()
        return self.records.get(name.casefold())

    async def list_openai_oauth_accounts(self):
        return [{"id": 285, "name": self.reference["name"], "status": "active"}]

    async def import_data(self, payload):
        for item in payload["accounts"]:
            name = item["name"]
            self.records[name.casefold()] = {
                **item, "id": len(self.records) + 1, "name": name,
                "status": item.get("status", "active"),
                "extra": item.get("extra", {}).copy(),
                "credentials_status": {"has_access_token": True, "has_refresh_token": True},
            }
        return {"account_created": len(payload["accounts"]), "account_failed": 0, "errors": []}

    async def get_account(self, account_id):
        if account_id == 285:
            return self.reference.copy()
        return next(value.copy() for value in self.records.values() if value["id"] == account_id)

    async def get_profile_account(self, account_id):
        return await self.get_account(account_id)

    async def get_usage(self, account_id):
        account = await self.get_account(account_id)
        extra = account.get("extra") or {}
        return {
            "updated_at": extra.get("codex_usage_updated_at"),
            "seven_day": {
                "utilization": extra.get("codex_7d_used_percent"),
                "resets_at": extra.get("codex_7d_reset_at"),
            },
        }

    async def list_active_proxies(self):
        return [
            {"id": 17382, "name": "5x", "protocol": "http", "host": "proxy.example",
             "port": 8080, "username": "u", "password": "p", "status": "active"},
            {"id": 999, "name": "other", "protocol": "http", "host": "other.example",
             "port": 8080, "status": "active"},
        ]

    async def update_groups(self, account_id, group_ids):
        account = next(value for value in self.records.values() if value["id"] == account_id)
        account["group_ids"] = group_ids

    async def update_settings(self, account_id, settings):
        self.settings_updates.append(settings.copy())
        account = next(value for value in self.records.values() if value["id"] == account_id)
        account.update(settings)

    async def set_schedulable(self, account_id, schedulable):
        account = next(value for value in self.records.values() if value["id"] == account_id)
        account["schedulable"] = schedulable

    async def apply_oauth(self, account_id, account):
        self.applied.append((account_id, account["credentials"]["access_token"]))
        target = next(value for value in self.records.values() if value["id"] == account_id)
        target["status"] = "active"
        target["error_message"] = ""

    async def list_groups(self):
        return [{"id": 2, "name": "OpenAI", "platform": "openai"}]

    async def close(self):
        self.closed = True


class WebServiceTests(unittest.IsolatedAsyncioTestCase):
    async def test_file_import_invalid_status_and_reupload(self):
        client = FakeClient()
        app = create_app(client, store=FakeStore(), watch_seconds=2, poll_seconds=0.01)
        transport = httpx.ASGITransport(app=app)
        async with httpx.AsyncClient(transport=transport, base_url="http://localhost") as http:
            page = await http.get("/")
            self.assertEqual(page.status_code, 200)
            csrf = re.search(r'content="([^"]+)"[^>]*name="csrf-token"', page.text)
            if not csrf:
                csrf = re.search(r'name="csrf-token" content="([^"]+)"', page.text)
            self.assertIsNotNone(csrf)
            invalid = await http.post(
                "/api/import/account",
                headers={"X-CSRF-Token": csrf.group(1)},
                json={"account_line": "secret", "group_ids": [2]},
            )
            self.assertEqual(invalid.status_code, 422)
            self.assertNotIn("secret", invalid.text)
            raw = json.dumps(bundle()).encode()
            kwargs = {"files": {"file": ("account.json", raw, "application/json")}}
            self.assertEqual((await http.post("/api/import/file", **kwargs)).status_code, 403)
            kwargs["data"] = {"profile_account_id": "285"}
            response = await http.post("/api/import/file", headers={"X-CSRF-Token": csrf.group(1)}, **kwargs)
            self.assertEqual(response.status_code, 202, response.text)
            job_id = response.json()[0]["id"]
            self.assertEqual(client.records["account@example.com"]["group_ids"], [2, 15])
            client.records["account@example.com"].update(status="error", error_message="OAuth token invalid")
            await asyncio.sleep(0.04)
            self.assertEqual(app.state.manager.get_job(job_id).state, "invalid")
            refreshed = json.dumps(bundle(token="token-two")).encode()
            response = await http.post(
                f"/api/jobs/{job_id}/reauthorize",
                headers={"X-CSRF-Token": csrf.group(1)},
                files={"file": ("new.json", refreshed, "application/json")},
            )
            self.assertEqual(response.status_code, 200, response.text)
            self.assertEqual(client.applied, [(1, "token-two")])
            self.assertEqual(response.json()["state"], "monitoring")
            public = (await http.get("/api/jobs")).text
            self.assertNotIn("token-two", public)
            self.assertNotIn("refresh-one", public)
        await app.state.manager.close()
        self.assertTrue(client.closed)

    async def test_credentials_reauthorize_once_without_exposing_secret(self):
        client = FakeClient()
        login_calls = []

        def login(line, fmt, *, group_ids, proxy):
            login_calls.append((fmt, group_ids, proxy))
            return bundle(name="account@example.com", token="new-token")

        manager = JobManager(client, store=FakeStore(), login=login, watch_seconds=2, poll_seconds=0.01, retry_seconds=0)
        line = "account@example.com----password----JBSWY3DPEHPK3PXP"
        job = await manager.import_credentials(line, [2], profile_account_id=285)
        for _ in range(40):
            if manager.get_job(job["id"]).account_id:
                break
            await asyncio.sleep(0.01)
        self.assertEqual(manager.get_job(job["id"]).account_id, 1)
        client.records["account@example.com"].update(status="error", error_message="401 invalid token")
        for _ in range(40):
            if client.applied:
                break
            await asyncio.sleep(0.01)
        self.assertEqual(client.applied, [(1, "new-token")])
        self.assertEqual(manager.get_job(job["id"]).attempts, 1)
        self.assertEqual(len(login_calls), 2)
        self.assertTrue(login_calls[0][2].startswith("http://"))
        self.assertNotIn("password", json.dumps(manager.list_jobs()))
        self.assertNotIn("JBSWY3DPEHPK3PXP", json.dumps(manager.list_jobs()))
        await manager.close()
        self.assertIsNone(manager.get_job(job["id"]).credential_line)

    def test_payload_requires_oauth_tokens(self):
        document = bundle()
        del document["accounts"][0]["credentials"]["refresh_token"]
        with self.assertRaises(ValueError):
            parse_bundle(json.dumps(document).encode())

    def test_unix_expiry_marks_account_invalid(self):
        self.assertEqual(_account_state({"status": "active", "expires_at": 1})[0], "invalid")

    def test_empty_5x_pool_means_direct_connection(self):
        self.assertIsNone(select_5x_proxy([{"id": 1, "name": "other", "status": "active"}]))

    async def test_multi_account_file_creates_separate_jobs(self):
        client = FakeClient()
        manager = JobManager(client, store=FakeStore(), watch_seconds=2, poll_seconds=0.01)
        document = bundle(name="first@example.com")
        document["accounts"].append(bundle(name="second@example.com")["accounts"][0])
        jobs = await manager.import_file(document, profile_account_id=285)
        self.assertEqual(len(jobs), 2)
        self.assertEqual({job["name"] for job in jobs}, {"first@example.com", "second@example.com"})
        self.assertEqual({job["account_id"] for job in jobs}, {1, 2})
        await manager.close()

    async def test_new_account_inherits_reference_settings_and_watches_calls(self):
        client = FakeClient()
        manager = JobManager(client, store=FakeStore(), watch_seconds=2, poll_seconds=0.01)
        watched = await manager.watch_reference(285)
        await asyncio.sleep(0.02)
        observed = manager.get_job(watched["id"]).public()
        self.assertEqual(observed["account_id"], 285)
        self.assertEqual(observed["current_concurrency"], 3)
        self.assertEqual(observed["state"], "monitoring")
        document = bundle()
        document["accounts"][0].update(load_factor=8, concurrency=4, expires_at=1)
        document["accounts"][0]["extra"]["openai_long_context_billing_enabled"] = False
        jobs = await manager.import_file(document, profile_account_id=285)
        self.assertEqual(jobs[0]["group_ids"], [2, 15])
        self.assertEqual(client.records["account@example.com"]["proxy_id"], 17382)
        self.assertEqual(client.records["account@example.com"]["concurrency"], 30)
        self.assertNotIn("load_factor", client.records["account@example.com"])
        self.assertNotIn("expires_at", client.records["account@example.com"])
        self.assertTrue(client.records["account@example.com"]["extra"]["openai_long_context_billing_enabled"])
        self.assertNotIn("email_key", client.records["account@example.com"]["extra"])
        await manager.close()

    async def test_credentials_default_to_reference_profile(self):
        client = FakeClient()
        login = lambda line, fmt, *, group_ids, proxy: bundle(token="fresh-token")
        manager = JobManager(client, store=FakeStore(), login=login, watch_seconds=2, poll_seconds=0.01)
        job = await manager.import_credentials("account@example.com----password----JBSWY3DPEHPK3PXP", profile_account_id=285)
        for _ in range(40):
            if manager.get_job(job["id"]).account_id:
                break
            await asyncio.sleep(0.01)
        record = client.records["account@example.com"]
        self.assertEqual(record["group_ids"], [2, 15])
        self.assertEqual(record["proxy_id"], 17382)
        self.assertTrue(record["schedulable"])
        await manager.close()

    async def test_existing_account_upload_reauthorizes_without_duplicate(self):
        client = FakeClient()
        await client.import_data(bundle())
        manager = JobManager(client, store=FakeStore(), watch_seconds=2, poll_seconds=0.01)
        refreshed = bundle(token="fresh-token")
        refreshed["accounts"][0]["group_ids"] = []
        jobs = await manager.import_file(refreshed)
        self.assertEqual(jobs[0]["account_id"], 1)
        self.assertEqual(jobs[0]["state"], "monitoring")
        self.assertEqual(len(client.records), 1)
        self.assertEqual(client.applied, [(1, "fresh-token")])
        await manager.close()

    async def test_existing_account_credentials_reauthorize(self):
        client = FakeClient()
        await client.import_data(bundle())
        login = lambda line, fmt, *, group_ids, proxy: bundle(token="fresh-token")
        manager = JobManager(client, store=FakeStore(), login=login, watch_seconds=2, poll_seconds=0.01)
        job = await manager.import_credentials("account@example.com----password----JBSWY3DPEHPK3PXP", [2])
        for _ in range(40):
            if client.applied:
                break
            await asyncio.sleep(0.01)
        self.assertEqual(client.applied, [(1, "fresh-token")])
        self.assertEqual(manager.get_job(job["id"]).state, "monitoring")
        self.assertEqual(len(client.records), 1)
        await manager.close()

    async def test_admin_client_uses_import_and_oauth_update_contract(self):
        seen = []

        def respond(request):
            seen.append((request.method, request.url.path, request.headers.get("x-api-key"), request.content))
            if request.url.path.endswith("/accounts"):
                data = {"items": [{"id": 3, "name": "account@example.com"}]}
            elif request.url.path.endswith("/accounts/3") and request.method == "GET":
                data = {"id": 3, "status": "active"}
            elif request.url.path.endswith("/accounts/data"):
                data = {"account_created": 1, "account_failed": 0}
            else:
                data = {}
            return httpx.Response(200, json={"code": 0, "data": data})

        client = Sub2APIClient("https://sub2api.example", "test-key", httpx.MockTransport(respond))
        self.assertEqual((await client.find_account("account@example.com"))["id"], 3)
        await client.import_data(bundle())
        await client.update_groups(3, [2])
        await client.apply_oauth(3, bundle()["accounts"][0])
        self.assertEqual((await client.get_account(3))["status"], "active")
        self.assertTrue(all(entry[2] == "test-key" for entry in seen))
        imported = json.loads(next(entry[3] for entry in seen if entry[1].endswith("/accounts/data")))
        self.assertTrue(imported["skip_default_group_bind"])
        updated = json.loads(next(entry[3] for entry in seen if entry[1].endswith("/apply-oauth-credentials")))
        self.assertEqual(updated["credentials"]["refresh_token"], "refresh-one")
        await client.close()


if __name__ == "__main__":
    unittest.main()
