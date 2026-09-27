from fake_store import FakeStore
import asyncio
import unittest

import httpx

from job_manager import JobManager, _account_profile, _prepared_new_account
from test_web_service import FakeClient, bundle
from web_service import DEFAULT_REFERENCE_ACCOUNT, create_app


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
    async def test_default_is_named_account_independent_of_list_order(self):
        client = FakeClient()

        async def accounts():
            return [
                {"id": 285, "name": "other@example.com"},
                {"id": 292, "name": DEFAULT_REFERENCE_ACCOUNT.upper()},
            ]

        client.list_openai_oauth_accounts = accounts
        app = create_app(client, store=FakeStore())
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://localhost") as http:
            response = await http.get("/api/config")
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["default_profile_account_id"], 292)
        await app.state.manager.close()

    async def test_missing_default_does_not_select_another_account(self):
        app = create_app(FakeClient(), store=FakeStore())
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://localhost") as http:
            response = await http.get("/api/config")
        self.assertIsNone(response.json()["default_profile_account_id"])
        self.assertEqual(response.json()["default_profile_account_name"], DEFAULT_REFERENCE_ACCOUNT)
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
