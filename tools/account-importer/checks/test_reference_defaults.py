from fake_store import FakeStore, wait_for_operation
import asyncio
import json
import re
import unittest

import httpx

from job_manager import JobManager, _account_profile, _prepared_new_account
from test_web_service import FakeClient, bundle
from default_profile import DEFAULT_PROFILE_ID, DEFAULT_PROFILE_NAME, default_import_profile
from sub2api_client import AdminAPIError
from web_service import create_app


def configured_client():
    client = FakeClient()
    client.reference.update(load_factor=1000, concurrency=30, priority=1)
    client.reference["credentials"] = {
        "model_mapping": {"gpt-5.5": "gpt-5.5", "gpt-6-sol": "gpt-6-sol"},
        "compact_model_mapping": {"gpt-5.5": "gpt-6-sol"},
        "access_token": "reference-token-must-not-copy",
        "refresh_token": "reference-refresh-must-not-copy",
        "chatgpt_account_id": "reference-id-must-not-copy",
    }
    client.reference["extra"].update(
        auto_reset_credit_enabled=True, auto_reset_credit_5h_threshold=91,
        auto_reset_credit_7d_threshold=92, codex_5h_used_percent=80,
    )
    return client


class ReferenceDefaultsTests(unittest.IsolatedAsyncioTestCase):
    async def test_default_option_and_models_do_not_require_any_reference_account(self):
        for unavailable in (False, True):
            client = FakeClient()
            async def accounts():
                if unavailable:
                    raise AdminAPIError("accounts unavailable")
                return []
            async def no_profile_lookup(account_id):
                raise AssertionError("Built-in defaults must not read a source account")
            client.list_openai_oauth_accounts = accounts
            client.get_profile_account = no_profile_lookup
            app = create_app(client, store=FakeStore())
            try:
                async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://localhost") as http:
                    config = (await http.get("/api/config")).json()
                    self.assertEqual(config["default_profile"], {"id": 0, "name": "默认"})
                    self.assertEqual(config["accounts"], [])
                    self.assertNotIn("default_profile_account_name", config)
                    response = await http.get("/api/reference/0/models")
                    self.assertEqual(response.status_code, 200)
                    self.assertEqual(response.json()["model_mapping"], default_import_profile()["credentials"]["model_mapping"])
                    self.assertEqual(response.json()["model_mapping"]["gpt-6.1-sol"], "gpt-6.1-sol")
                    self.assertEqual(len(response.json()["model_mapping"]), 10)
            finally:
                await app.state.manager.close()

    async def test_both_modes_use_frozen_defaults_when_source_account_is_deleted(self):
        expected = default_import_profile()
        for mode in ("file", "credentials"):
            for explicit in (False, True):
                with self.subTest(mode=mode, explicit=explicit):
                    client = FakeClient()
                    async def no_source_lookup(account_id):
                        raise AssertionError("Source account was deleted")
                    client.get_profile_account = no_source_lookup
                    document = bundle(token="own-token")
                    manager = JobManager(client, store=FakeStore(), login=lambda *a, **kw: document)
                    try:
                        options = {"profile_account_id": DEFAULT_PROFILE_ID} if explicit else {}
                        if mode == "file":
                            public = (await manager.import_file(document, **options))[0]
                        else:
                            public = await manager.import_credentials("account@example.com----password----JBSWY3DPEHPK3PXP", **options)
                            await wait_for_operation(manager.get_job(public["id"]))
                        job = manager.get_job(public["id"])
                        self.assertEqual(job.state, "monitoring")
                        self.assertEqual(job.profile_account_id, DEFAULT_PROFILE_ID)
                        record = client.records["account@example.com"]
                        self.assertEqual(record["group_ids"], expected["group_ids"])
                        for key, value in expected["settings"].items():
                            self.assertEqual(record[key], value)
                        for key, value in expected["extra"].items():
                            self.assertEqual(record["extra"][key], value)
                        self.assertEqual(record["credentials"]["model_mapping"], expected["credentials"]["model_mapping"])
                        self.assertEqual(record["credentials"]["access_token"], "own-token")
                        self.assertEqual(record["status"], "active")
                        self.assertEqual(record["proxy_id"], 17382)
                    finally:
                        await manager.close()

    async def test_failed_default_import_can_retry_after_restart_without_source_account(self):
        client, store = FakeClient(), FakeStore()
        async def no_source_lookup(account_id):
            raise AssertionError("Source account was deleted")
        client.get_profile_account = no_source_lookup
        def fail(*args, **kwargs):
            raise RuntimeError("login unavailable")
        first = JobManager(client, store=store, login=fail)
        public = await first.import_credentials("account@example.com----password----JBSWY3DPEHPK3PXP")
        await wait_for_operation(first.get_job(public["id"]))
        self.assertEqual(first.get_job(public["id"]).state, "failed")
        await first.close()
        second = JobManager(client, store=store, login=lambda *a, **kw: bundle())
        try:
            await second.start()
            await second.reauthorize_credentials(public["id"])
            job = second.get_job(public["id"])
            await wait_for_operation(job)
            self.assertEqual(job.state, "monitoring")
            self.assertEqual(job.profile_account_id, DEFAULT_PROFILE_ID)
            self.assertEqual(client.records["account@example.com"]["group_ids"], default_import_profile()["group_ids"])
        finally:
            await second.close()

    async def test_group_override_does_not_mutate_default_profile(self):
        manager = JobManager(FakeClient(), store=FakeStore())
        try:
            jobs = await manager.import_file(bundle(), override_group_ids=[42])
            self.assertEqual(jobs[0]["group_ids"], [42])
            self.assertEqual(default_import_profile()["group_ids"], [2, 5, 7, 15, 6, 8, 13])
        finally:
            await manager.close()

    def test_default_profile_has_no_account_identity_or_secrets_and_returns_independent_copies(self):
        profile = default_import_profile()
        self.assertEqual(DEFAULT_PROFILE_NAME, "默认")
        self.assertEqual(set(profile), {"settings", "group_ids", "extra", "credentials"})
        self.assertEqual(set(profile["credentials"]), {"model_mapping"})
        for key in ("proxy_id", "status", "expires_at", "account_id"):
            self.assertNotIn(key, profile["settings"])
        profile["group_ids"].clear()
        profile["credentials"]["model_mapping"].clear()
        self.assertEqual(len(default_import_profile()["group_ids"]), 7)
        self.assertEqual(len(default_import_profile()["credentials"]["model_mapping"]), 10)

    async def test_api_imports_use_builtin_default_with_omitted_or_zero_selection(self):
        for mode in ("file", "credentials"):
            for explicit in (False, True):
                client = FakeClient()
                async def no_source_lookup(account_id):
                    raise AssertionError("API must not resolve the deleted source account")
                client.get_profile_account = no_source_lookup
                app = create_app(client, store=FakeStore(), login=lambda *a, **kw: bundle())
                try:
                    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://localhost") as http:
                        page = await http.get("/")
                        csrf = re.search(r'name="csrf-token" content="([^"]+)"', page.text).group(1)
                        headers = {"X-CSRF-Token": csrf}
                        if mode == "file":
                            response = await http.post("/api/import/file", headers=headers,
                                data={"profile_account_id": "0"} if explicit else {},
                                files={"file": ("account.json", json.dumps(bundle()), "application/json")})
                            job_id = response.json()[0]["id"]
                        else:
                            path = "/api/import/account" + ("?profile_account_id=0" if explicit else "")
                            response = await http.post(path, headers=headers, json={"account_line": "account@example.com----password----JBSWY3DPEHPK3PXP"})
                            job_id = response.json()["id"]
                        self.assertEqual(response.status_code, 202, response.text)
                        await wait_for_operation(app.state.manager.get_job(job_id))
                        record = client.records["account@example.com"]
                        self.assertEqual(record["group_ids"], default_import_profile()["group_ids"])
                        self.assertEqual(record["credentials"]["model_mapping"], default_import_profile()["credentials"]["model_mapping"])
                finally:
                    await app.state.manager.close()

    async def test_both_import_modes_inherit_configuration_with_own_tokens(self):
        for mode in ("file", "credentials"):
            with self.subTest(mode=mode):
                client = configured_client()
                document = bundle(token="new-account-token")
                document["accounts"][0]["credentials"].update(
                    chatgpt_account_id="new-account-id",
                    model_mapping={"uploaded-model": "uploaded-model"},
                )
                manager = JobManager(client, store=FakeStore(), login=lambda *args, **kwargs: document, poll_seconds=0.01)
                try:
                    if mode == "file":
                        jobs = await manager.import_file(document, profile_account_id=285)
                        job_id = jobs[0]["id"]
                    else:
                        job = await manager.import_credentials(
                            "account@example.com----password----JBSWY3DPEHPK3PXP", profile_account_id=285,
                        )
                        job_id = job["id"]
                        for _ in range(50):
                            if manager.get_job(job_id).state != "importing":
                                break
                            await asyncio.sleep(0.01)
                    self.assertEqual(manager.get_job(job_id).state, "monitoring")
                    record = client.records["account@example.com"]
                    self.assertEqual(record["concurrency"], 30)
                    self.assertEqual(record["priority"], 1)
                    self.assertEqual(record["load_factor"], 1000)
                    self.assertEqual(record["rate_multiplier"], 1)
                    self.assertEqual(record["group_ids"], [2, 15])
                    for key in ("model_mapping", "compact_model_mapping"):
                        self.assertEqual(record["credentials"][key], client.reference["credentials"][key])
                    self.assertEqual(record["credentials"]["access_token"], "new-account-token")
                    self.assertEqual(record["credentials"]["refresh_token"], "refresh-one")
                    self.assertEqual(record["credentials"]["chatgpt_account_id"], "new-account-id")
                    self.assertEqual(record["extra"]["auto_reset_credit_5h_threshold"], 91)
                    self.assertEqual(record["extra"]["auto_reset_credit_7d_threshold"], 92)
                    self.assertNotIn("codex_5h_used_percent", record["extra"])
                finally:
                    await manager.close()

    def test_model_settings_are_independent_copies(self):
        client = configured_client()
        profile = _account_profile(client.reference)
        prepared = _prepared_new_account(bundle()["accounts"][0], profile, [99])
        self.assertEqual(prepared["group_ids"], [99])
        prepared["credentials"]["model_mapping"]["gpt-5.5"] = "changed"
        self.assertEqual(profile["credentials"]["model_mapping"]["gpt-5.5"], "gpt-5.5")
        profile["credentials"]["model_mapping"]["gpt-5.5"] = "profile-change"
        self.assertEqual(client.reference["credentials"]["model_mapping"]["gpt-5.5"], "gpt-5.5")

    def test_unset_reference_models_reject_import(self):
        client = FakeClient()
        client.reference['credentials'] = {}
        profile = _account_profile(client.reference)
        with self.assertRaisesRegex(ValueError, '未设置模型限制'):
            _prepared_new_account(bundle()['accounts'][0], profile, profile['group_ids'])


if __name__ == "__main__":
    unittest.main()
