import json
import logging
import os
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import local_relogin


class FakeFlow:
    def __init__(self, config, env_overrides, account_callback):
        self.result = SimpleNamespace(totp_secret="")
        self.account_callback = account_callback
        self.env_overrides = env_overrides

    def run_protocol_login(self, provider, email, password):
        assert self.result.totp_secret == "JBSWY3DPEHPK3PXP"
        assert self.account_callback(email)["password"] == password
        assert self.env_overrides["OAUTH_REFRESH_ONLY"] == "1"
        return SimpleNamespace(
            access_token="fake-access", refresh_token="fake-refresh", id_token="fake-id"
        )


class LocalReloginTests(unittest.TestCase):
    def test_parse_account_line(self):
        line = "user@example.com----pass----JBSWY3DPEHPK3PXP"
        self.assertEqual(local_relogin.parse_account_line(line)[0], "user@example.com")
        with self.assertRaises(ValueError):
            local_relogin.parse_account_line("user@example.com----pass")
        with self.assertRaisesRegex(ValueError, "Base32"):
            local_relogin.parse_account_line("user@example.com----pass----not-base32!")

    def test_export_formats_and_permissions(self):
        fake_backend = (
            FakeFlow, lambda proxy: proxy, lambda email, reason: None,
            lambda account: {"type": "codex", "access_token": account.access_token},
            lambda account, group_ids: {"name": account.email, "credentials": {
                "access_token": account.access_token, "refresh_token": account.refresh_token
            }},
        )
        with tempfile.TemporaryDirectory() as folder, patch.object(
            local_relogin, "_load_upstream", return_value=fake_backend
        ):
            for fmt in local_relogin.EXPORT_FORMATS:
                destination = local_relogin.relogin_and_download(
                    "user@example.com----pass----JBSWY3DPEHPK3PXP", Path(folder), fmt
                )
                self.assertEqual(os.stat(destination).st_mode & 0o777, 0o600)
                data = json.loads(destination.read_text(encoding="utf-8"))
                self.assertNotIn("pass", json.dumps(data))
                self.assertNotIn("JBSWY3DPEHPK3PXP", json.dumps(data))
                if fmt == "sub2api":
                    self.assertEqual(data["accounts"][0]["credentials"]["refresh_token"], "fake-refresh")
                with self.assertRaises(FileExistsError):
                    local_relogin.relogin_and_download(
                        "user@example.com----pass----JBSWY3DPEHPK3PXP", Path(folder), fmt
                    )

    def test_disabled_account_uses_upstream_403_reason(self):
        class DisabledFlow(FakeFlow):
            def run_protocol_login(self, provider, email, password):
                logging.getLogger("platforms.chatgpt.protocol.auth_flow").warning(
                    "TOTP verification failed 403: account deleted or deactivated"
                )
                raise RuntimeError("invalid_auth_step")

        fake_backend = (DisabledFlow, lambda proxy: proxy, lambda email, reason: None,
                        lambda account: {}, lambda account, group_ids: {})
        with tempfile.TemporaryDirectory() as folder, patch.object(
            local_relogin, "_load_upstream", return_value=fake_backend
        ):
            with self.assertRaisesRegex(RuntimeError, "账号已删除或停用"):
                local_relogin.relogin_and_download(
                    "user@example.com----pass----JBSWY3DPEHPK3PXP", Path(folder)
                )
            self.assertFalse(list(Path(folder).iterdir()))


if __name__ == "__main__":
    unittest.main()
