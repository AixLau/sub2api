import asyncio
import json
import re
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import httpx

import local_register
from fake_store import FakeStore
from job_manager import JobManager
from local_register import (
    RegistrationIncomplete,
    parse_registration_line,
    register_payload,
)
from mail_api_mailbox import MailAPIMailbox, extract_code, validate_mail_api_url
from test_web_service import FakeClient, bundle
from web_service import create_app

MAIL_URL = "https://mail.example/api/key=secret-token"
REGISTER_LINE = f"new@example.com----{MAIL_URL}"
TOTP = "JBSWY3DPEHPK3PXP"
PASSWORD = "generated-password"


class Response:
    def __init__(self, payload, status=200):
        self.status_code = status
        self._payload = payload
        self.text = payload if isinstance(payload, str) else json.dumps(payload, ensure_ascii=False)
        self.content = self.text.encode()

    def json(self):
        if isinstance(self._payload, str):
            raise ValueError("not json")
        return self._payload


class FakeClock:
    def __init__(self):
        self.value = 0.0

    def __call__(self):
        return self.value

    def sleep(self, seconds):
        self.value += seconds


class RegistrationLineTests(unittest.TestCase):
    def test_parse_registration_line(self):
        self.assertEqual(
            parse_registration_line(REGISTER_LINE),
            ("new@example.com", MAIL_URL),
        )
        self.assertEqual(
            parse_registration_line("new@example.com====https://mail.example/code"),
            ("new@example.com", "https://mail.example/code"),
        )

    def test_rejects_invalid_lines_without_echoing_secrets(self):
        for line in (
            "new@example.com",
            "not-an-email----https://mail.example/code",
            "new@example.com----ftp://mail.example/code",
            "new@example.com----https://user:pass@mail.example/code",
            "new@example.com----https://mail.example/code\nother@example.com----https://mail.example/code",
        ):
            with self.subTest(line=line), self.assertRaises(ValueError) as raised:
                parse_registration_line(line)
            self.assertNotIn("secret-token", str(raised.exception))
            self.assertNotIn("user:pass", str(raised.exception))

    def test_validate_mail_api_url(self):
        self.assertEqual(validate_mail_api_url(MAIL_URL), MAIL_URL)
        with self.assertRaises(ValueError):
            validate_mail_api_url("file:///etc/passwd")


class MailAPIMailboxTests(unittest.TestCase):
    def test_extracts_structured_and_text_codes(self):
        self.assertEqual(extract_code({"success": True, "code": "123456"}), "123456")
        self.assertEqual(extract_code({"data": {"verification_code": "222222"}}), "222222")
        self.assertEqual(extract_code({"message": "Your verification code is 654321"}), "654321")
        self.assertEqual(extract_code({"code": ""}, "<p>Code: 111111</p>"), "111111")
        self.assertEqual(
            extract_code({"ok": True, "code": None, "latest_code": "123456", "messages": []}),
            "123456",
        )
        self.assertEqual(
            extract_code({
                "ok": True,
                "latest_code": None,
                "messages": [{"subject": "Your code", "code": "654321"}],
            }),
            "654321",
        )

    def test_ignores_template_noise_and_prefers_context(self):
        body = "<style>color: #202123</style><p>Your code is 333333</p><p>202123</p>"
        self.assertEqual(extract_code(body), "333333")

    def test_ignores_numbers_inside_links(self):
        self.assertEqual(extract_code({"login_url": "https://auth.example/v/123456"}), "")
        self.assertEqual(
            extract_code("Open https://example.test/123456 or use code 222222"), "222222"
        )

    def test_primes_existing_code_and_returns_only_new_code(self):
        responses = iter([
            Response({"success": True, "code": "111111"}),
            Response({"success": False, "code": "", "message": "等待中"}),
            Response({"success": True, "code": "222222"}),
        ])

        def get(url, **kwargs):
            return next(responses)

        mailbox = MailAPIMailbox(
            "new@example.com", MAIL_URL, http_get=get, settle_seconds=0, poll_interval=0.01
        )
        self.assertEqual(mailbox.get_current_ids(object()), {"111111"})
        self.assertEqual(mailbox.wait_for_code(object(), timeout=5), "222222")
        # A consumed code is not handed out again on the next wait.
        mailbox.get_current_ids = lambda _account: set()
        self.assertEqual(mailbox._seen, {"111111", "222222"})

    def test_timeout_does_not_leak_api_url(self):
        clock = FakeClock()

        def get(url, **kwargs):
            return Response({"success": False, "code": "", "message": "暂无验证码"})

        mailbox = MailAPIMailbox(
            "new@example.com",
            MAIL_URL,
            http_get=get,
            settle_seconds=0,
            poll_interval=1,
            clock=clock,
            sleep=clock.sleep,
        )
        with self.assertRaises(TimeoutError) as raised:
            mailbox.wait_for_code(object(), timeout=2)
        self.assertNotIn(MAIL_URL, str(raised.exception))
        self.assertNotIn("secret-token", str(raised.exception))

    def test_cache_buster_is_applied(self):
        urls = []

        def get(url, **kwargs):
            urls.append(url)
            return Response({"success": True, "code": "123456"})

        mailbox = MailAPIMailbox(
            "new@example.com", MAIL_URL, http_get=get, settle_seconds=0, poll_interval=0.01
        )
        self.assertEqual(mailbox.wait_for_code(object(), timeout=2), "123456")
        self.assertIn("_otp_poll=", urls[0])


class FakeMailbox:
    instances = []

    def __init__(self, email, api_url, proxy=None):
        self.email = email
        self.api_url = api_url
        self.proxy = proxy
        self.closed = False
        FakeMailbox.instances.append(self)

    def close(self):
        self.closed = True


class FakeEngine:
    instances = []
    secret = TOTP

    def __init__(self, **kwargs):
        self.kwargs = kwargs
        self.password = kwargs["password"]
        self.flow = SimpleNamespace(result=SimpleNamespace(totp_secret=self.secret))
        self.two_factor = None
        FakeEngine.instances.append(self)

    def run(self):
        self.kwargs["log_fn"](
            "密码已在 OpenAI 侧生效: new@example.com / " + self.password
        )
        self.kwargs["log_fn"](
            "密码已在 OpenAI 侧生效: new@example.com / real-internal-password"
        )
        self.kwargs["log_fn"]("2FA 绑定成功，TOTP 密钥: " + self.secret)
        self.kwargs["log_fn"]("验证码 123456 已提交")
        return SimpleNamespace(
            success=True,
            email="new@example.com",
            password=self.password,
            access_token="registered-access",
            refresh_token="registered-refresh",
            id_token="registered-id",
            metadata={"totp_secret": self.secret},
        )


def make_sub2api(account, group_ids):
    return {
        "name": account.email,
        "platform": "openai",
        "type": "oauth",
        "credentials": {
            "access_token": account.access_token,
            "refresh_token": account.refresh_token,
            "id_token": account.id_token,
        },
        "extra": {"email": account.email},
        "group_ids": list(group_ids),
    }


class LocalRegisterTests(unittest.TestCase):
    def setUp(self):
        FakeEngine.instances.clear()
        FakeMailbox.instances.clear()

    def test_register_payload_binds_2fa_and_returns_credentials(self):
        fake_backend = (FakeEngine, "email", "refresh_token", make_sub2api)
        with patch.object(local_register, "_load_upstream", return_value=fake_backend), \
             self.assertLogs("account_import.timing", level="INFO") as logs:
            payload, credential_line = register_payload(
                REGISTER_LINE, mailbox_factory=FakeMailbox
            )

        engine = FakeEngine.instances[0]
        self.assertTrue(engine.kwargs["bind_2fa"])
        self.assertEqual(engine.kwargs["mailbox_kind"], "generic_api")
        self.assertEqual(engine.kwargs["register_flow"], "email")
        self.assertEqual(engine.kwargs["mode"], "refresh_token")
        self.assertEqual(FakeMailbox.instances[0].api_url, MAIL_URL)
        self.assertTrue(FakeMailbox.instances[0].closed)

        self.assertEqual(payload["accounts"][0]["name"], "new@example.com")
        self.assertEqual(payload["accounts"][0]["credentials"]["refresh_token"], "registered-refresh")
        self.assertEqual(credential_line, f"new@example.com----{engine.password}----{TOTP}")

        output = "\n".join(logs.output)
        self.assertNotIn(MAIL_URL, output)
        self.assertNotIn(engine.password, output)
        self.assertNotIn("real-internal-password", output)
        self.assertNotIn(TOTP, output)
        self.assertNotIn("123456", output)

    def test_uses_the_password_generated_inside_the_flow(self):
        class FlowPasswordEngine(FakeEngine):
            def run(self):
                actual = "flow-generated-password"
                self.kwargs["log_fn"](
                    "密码已在 OpenAI 侧生效: new@example.com / " + actual
                )
                return SimpleNamespace(
                    success=True,
                    email="new@example.com",
                    password=actual,
                    access_token="registered-access",
                    refresh_token="registered-refresh",
                    id_token="registered-id",
                    metadata={"totp_secret": TOTP},
                )

        fake_backend = (FlowPasswordEngine, "email", "refresh_token", make_sub2api)
        with patch.object(local_register, "_load_upstream", return_value=fake_backend), \
             self.assertLogs("account_import.timing", level="INFO") as logs:
            _payload, credential_line = register_payload(
                REGISTER_LINE, mailbox_factory=FakeMailbox
            )
        self.assertEqual(
            credential_line, f"new@example.com----flow-generated-password----{TOTP}"
        )
        self.assertNotIn("flow-generated-password", "\n".join(logs.output))

    def test_missing_refresh_token_preserves_credentials(self):
        class NoRefreshEngine(FakeEngine):
            def run(self):
                return SimpleNamespace(
                    success=True,
                    email="new@example.com",
                    password="flow-generated-password",
                    access_token="registered-access",
                    refresh_token="",
                    id_token="registered-id",
                    metadata={"totp_secret": TOTP},
                )

        fake_backend = (NoRefreshEngine, "email", "refresh_token", make_sub2api)
        with patch.object(local_register, "_load_upstream", return_value=fake_backend):
            with self.assertRaises(RegistrationIncomplete) as raised:
                register_payload(REGISTER_LINE, mailbox_factory=FakeMailbox)
        self.assertEqual(
            raised.exception.credential_line,
            f"new@example.com----flow-generated-password----{TOTP}",
        )
        self.assertTrue(raised.exception.two_factor_bound)

    def test_exception_after_2fa_preserves_credentials(self):
        class ExplodingEngine(FakeEngine):
            def run(self):
                self.flow.result.totp_secret = self.secret
                self.password = "flow-generated-password"
                raise RuntimeError("flow crashed")

        fake_backend = (ExplodingEngine, "email", "refresh_token", make_sub2api)
        with patch.object(local_register, "_load_upstream", return_value=fake_backend):
            with self.assertRaises(RegistrationIncomplete) as raised:
                register_payload(REGISTER_LINE, mailbox_factory=FakeMailbox)
        self.assertEqual(
            raised.exception.credential_line,
            f"new@example.com----flow-generated-password----{TOTP}",
        )
        self.assertNotIn(MAIL_URL, str(raised.exception))
        self.assertNotIn(TOTP, str(raised.exception))

    def test_failure_message_is_sanitized(self):
        class FailingEngine(FakeEngine):
            def run(self):
                self.flow.result.totp_secret = ""
                raise RuntimeError(f"failed {MAIL_URL} password={self.password}")

        fake_backend = (FailingEngine, "email", "refresh_token", make_sub2api)
        with patch.object(local_register, "_load_upstream", return_value=fake_backend):
            with self.assertRaises(RuntimeError) as raised:
                register_payload(REGISTER_LINE, mailbox_factory=FakeMailbox)
        message = str(raised.exception)
        self.assertNotIn(MAIL_URL, message)
        self.assertNotIn("secret-token", message)
        self.assertNotIn(FailingEngine.instances[0].password, message)

    def test_missing_totp_still_imports_without_credential_line(self):
        class NoTotpEngine(FakeEngine):
            secret = ""

        fake_backend = (NoTotpEngine, "email", "refresh_token", make_sub2api)
        with patch.object(local_register, "_load_upstream", return_value=fake_backend):
            payload, credential_line = register_payload(
                REGISTER_LINE, mailbox_factory=FakeMailbox
            )
        self.assertIsNone(credential_line)
        self.assertEqual(payload["accounts"][0]["name"], "new@example.com")


def successful_register(line, fmt, *, group_ids, proxy):
    return (
        bundle(name="new@example.com", token="registered-token"),
        f"new@example.com----{PASSWORD}----{TOTP}",
    )


class JobRegistrationTests(unittest.IsolatedAsyncioTestCase):
    async def until(self, predicate):
        async with asyncio.timeout(3):
            while not predicate():
                await asyncio.sleep(0.005)

    async def test_registration_imports_and_hides_secrets(self):
        client = FakeClient()
        manager = JobManager(
            client, store=FakeStore(), register=successful_register, poll_seconds=0.01
        )
        job = await manager.register_credentials(REGISTER_LINE, [], profile_account_id=285)
        await self.until(lambda: manager.get_job(job["id"]).account_id is not None)
        current = manager.get_job(job["id"])

        record = client.records["new@example.com"]
        self.assertEqual(record["group_ids"], [2, 15])
        self.assertEqual(record["proxy_id"], 17382)
        self.assertTrue(record["schedulable"])
        self.assertEqual(current.credential_line, f"new@example.com----{PASSWORD}----{TOTP}")
        self.assertIsNone(current.registration_line)
        self.assertTrue(current.two_factor_bound)
        self.assertEqual(current.state, "monitoring")

        public = json.dumps(current.public())
        self.assertNotIn(MAIL_URL, public)
        self.assertNotIn("secret-token", public)
        self.assertNotIn(PASSWORD, public)
        self.assertNotIn(TOTP, public)
        self.assertFalse(current.public()["can_retry_registration"])
        await manager.close()

    async def test_registration_failure_can_be_retried(self):
        client = FakeClient()

        def fail(line, fmt, *, group_ids, proxy):
            raise RuntimeError("收信接口未返回验证码")

        manager = JobManager(client, store=FakeStore(), register=fail, poll_seconds=0.01)
        job = await manager.register_credentials(REGISTER_LINE, [2], profile_account_id=285)
        await self.until(lambda: manager.get_job(job["id"]).state == "failed")
        failed = manager.get_job(job["id"])
        self.assertTrue(failed.public()["can_retry_registration"])
        self.assertIsNotNone(failed.registration_line)
        self.assertNotIn("secret-token", json.dumps(failed.public()))

        manager.register = successful_register
        await manager.retry_registration(job["id"])
        await self.until(lambda: manager.get_job(job["id"]).account_id is not None)
        retried = manager.get_job(job["id"])
        self.assertEqual(retried.state, "monitoring")
        self.assertIsNone(retried.registration_line)
        self.assertTrue(retried.two_factor_bound)
        await manager.close()

    async def test_registration_secret_survives_restart_for_retry(self):
        client = FakeClient()

        def fail(line, fmt, *, group_ids, proxy):
            raise RuntimeError("收信接口未返回验证码")

        store = FakeStore()
        manager = JobManager(client, store=store, register=fail, poll_seconds=0.01)
        job = await manager.register_credentials(REGISTER_LINE, [2], profile_account_id=285)
        await self.until(lambda: manager.get_job(job["id"]).state == "failed")
        manager.jobs.clear()

        await manager.start()
        restored = manager.get_job(job["id"])
        self.assertEqual(restored.registration_line, REGISTER_LINE)
        self.assertTrue(restored.public()["can_retry_registration"])
        await manager.close()

    async def test_partial_registration_keeps_credentials_and_can_import(self):
        client = FakeClient()

        def partial(line, fmt, *, group_ids, proxy):
            raise RegistrationIncomplete(
                "注册已创建账号，但未取得 refresh_token",
                credential_line=f"new@example.com----{PASSWORD}----{TOTP}",
                two_factor_bound=True,
            )

        manager = JobManager(client, store=FakeStore(), register=partial, poll_seconds=0.01)
        job = await manager.register_credentials(REGISTER_LINE, [2], profile_account_id=285)
        await self.until(lambda: manager.get_job(job["id"]).state == "attention")
        current = manager.get_job(job["id"])
        self.assertTrue(current.public()["has_saved_credentials"])
        self.assertTrue(current.two_factor_bound)
        self.assertIsNone(current.registration_line)
        self.assertFalse(current.public()["can_retry_registration"])

        manager.login = lambda line, fmt, *, group_ids, proxy: bundle(
            name="new@example.com", token="imported-token"
        )
        await manager.reauthorize_credentials(job["id"])
        await self.until(lambda: manager.get_job(job["id"]).account_id is not None)
        imported = manager.get_job(job["id"])
        self.assertEqual(imported.state, "monitoring")
        self.assertEqual(
            client.records["new@example.com"]["credentials"]["access_token"],
            "imported-token",
        )
        self.assertTrue(imported.public()["has_saved_credentials"])
        await manager.close()

    async def test_registered_account_uses_auto_relogin(self):
        client = FakeClient()
        manager = JobManager(
            client, store=FakeStore(), register=successful_register, poll_seconds=0.01
        )
        job = await manager.register_credentials(REGISTER_LINE, [2], profile_account_id=285)
        await self.until(lambda: manager.get_job(job["id"]).account_id is not None)
        current = manager.get_job(job["id"])

        client.records["new@example.com"].update(status="error", error_message="401 invalid token")
        manager.login = lambda line, fmt, *, group_ids, proxy: bundle(
            name="new@example.com", token="reauth-token"
        )
        await self.until(lambda: client.applied)
        self.assertEqual(client.applied, [(1, "reauth-token")])
        self.assertEqual(current.attempts, 1)
        await manager.close()


class RegistrationWebTests(unittest.IsolatedAsyncioTestCase):
    async def csrf(self, http):
        page = await http.get("/")
        match = re.search(r'name="csrf-token" content="([^"]+)"', page.text)
        self.assertIsNotNone(match)
        return match.group(1)

    async def test_register_endpoint_requires_csrf_and_valid_line(self):
        client = FakeClient()
        app = create_app(
            client, store=FakeStore(), register=successful_register, poll_seconds=0.01
        )
        transport = httpx.ASGITransport(app=app)
        async with httpx.AsyncClient(transport=transport, base_url="http://localhost") as http:
            body = {
                "registration_lines": [REGISTER_LINE],
                "group_ids": [2],
                "plugin_id": None,
            }
            response = await http.post("/api/register", json=body)
            self.assertEqual(response.status_code, 403)

            token = await self.csrf(http)
            response = await http.post(
                "/api/register?profile_account_id=285",
                headers={"X-CSRF-Token": token},
                json=body,
            )
            self.assertEqual(response.status_code, 202, response.text)
            jobs = response.json()
            self.assertEqual(len(jobs), 1)
            job_id = jobs[0]["id"]
            await asyncio.sleep(0.05)
            public = await http.get("/api/jobs")
            self.assertNotIn(MAIL_URL, public.text)
            self.assertNotIn("secret-token", public.text)
            self.assertNotIn(PASSWORD, public.text)
            self.assertNotIn(TOTP, public.text)

            invalid = await http.post(
                "/api/register?profile_account_id=285",
                headers={"X-CSRF-Token": token},
                json={"registration_lines": ["not-a-line"], "group_ids": [], "plugin_id": None},
            )
            self.assertEqual(invalid.status_code, 400)
            self.assertNotIn("secret-token", invalid.text)
            self.assertEqual(app.state.manager.get_job(job_id).account_id, 1)
        await app.state.manager.close()


if __name__ == "__main__":
    unittest.main()
