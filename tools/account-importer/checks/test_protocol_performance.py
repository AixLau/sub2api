"""Exercise the patched upstream protocol without contacting OpenAI."""
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import Mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from local_relogin import DEFAULT_SOURCE_DIR, _load_upstream

UPSTREAM_AVAILABLE = (DEFAULT_SOURCE_DIR / 'platforms/chatgpt/protocol/auth_flow.py').is_file()


@unittest.skipUnless(UPSTREAM_AVAILABLE, 'Requires the pinned protocol source in the importer image')
class ProtocolPerformanceTests(unittest.TestCase):
    def test_sentinel_deadlines_clear_on_success_rejection_and_timeout(self):
        source = (DEFAULT_SOURCE_DIR / 'platforms/chatgpt/protocol/openai_sentinel_quickjs.js').read_text()
        helper = source[source.index('async function withTimeout('):source.index('// ─── Main')]
        script = helper + '''
(async () => {
  const assert = require('node:assert/strict');
  assert.equal(await withTimeout(Promise.resolve('ready'), 8000, 'deadline'), 'ready');
  await assert.rejects(withTimeout(Promise.reject(new Error('upstream')), 8000, 'deadline'), /upstream/);
  await assert.rejects(withTimeout(new Promise(() => {}), 20, 'deadline'), /deadline/);
  process.stdout.write('ok');
})().catch(error => { process.stderr.write(String(error)); process.exitCode = 1; });
'''
        # Any leaked eight-second timer makes the process exceed this deadline.
        result = subprocess.run(['node', '-e', script], capture_output=True, text=True, timeout=2)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, 'ok')

    def make_flow(self, *, refresh_only, access, refresh):
        AuthFlow, *_ = _load_upstream(DEFAULT_SOURCE_DIR)
        flow = AuthFlow.__new__(AuthFlow)
        from platforms.chatgpt.protocol.auth_flow import AuthResult
        flow.result = AuthResult()
        flow.result.totp_secret = 'JBSWY3DPEHPK3PXP'
        flow._env_overrides = {
            'OAUTH_REFRESH_ONLY': str(int(refresh_only)),
            'OAUTH_CODEX_RT_BEFORE_CALLBACK': '1',
            'OAUTH_CODEX_AFTER_CALLBACK': str(int(refresh_only)),
        }
        flow._account_callback = None
        for name, value in {
            'check_proxy': True, 'warmup': True, 'get_csrf_token': 'csrf',
            'get_auth_url': 'https://auth.example/authorize', 'auth_oauth_init': 'device',
            'get_sentinel_token': 'sentinel',
            'authorize_continue': {'page': {'type': 'login_password'}, 'continue_url': '/log-in/password'},
            'login_password_verify': {'page': {'type': 'mfa_challenge'}, 'continue_url': '/mfa-challenge/challenge'},
            'submit_mfa_totp': {'continue_url': 'https://auth.example/continue'},
            'follow_redirect_chain': ('https://web.example/callback', 'https://web.example/callback'),
            'fetch_client_auth_session_dump': {},
        }.items():
            setattr(flow, name, Mock(return_value=value))
        flow._normalize_continue_url = Mock(side_effect=lambda url: url)
        flow._consume_callback_for_session = Mock(return_value=True)

        def exchange(**kwargs):
            flow.result.access_token = access
            flow.result.refresh_token = refresh
            return bool(access and refresh)
        flow.oauth_codex_rt_exchange = Mock(side_effect=exchange)
        flow.get_auth_session = Mock(side_effect=lambda: setattr(flow.result, 'session_token', 'session'))
        provider = Mock()
        provider.wait_for_otp.side_effect = AssertionError('TOTP login must not poll email')
        return flow, provider

    def test_complete_oauth_skips_web_callback_and_dump_after_password_and_totp(self):
        flow, provider = self.make_flow(refresh_only=True, access='access', refresh='refresh')
        result = flow.run_protocol_login(provider, 'user@example.com', 'password')
        self.assertEqual((result.access_token, result.refresh_token), ('access', 'refresh'))
        flow.login_password_verify.assert_called_once_with('password')
        flow.submit_mfa_totp.assert_called_once()
        flow.oauth_codex_rt_exchange.assert_called_once()
        flow.follow_redirect_chain.assert_called_once()
        flow._consume_callback_for_session.assert_called_once_with('https://web.example/callback')
        flow.fetch_client_auth_session_dump.assert_not_called()
        flow.get_auth_session.assert_called_once()

    def test_incomplete_credentials_continue_existing_flow(self):
        for access, refresh in [('access', ''), ('', 'refresh')]:
            with self.subTest(access=bool(access), refresh=bool(refresh)):
                flow, provider = self.make_flow(refresh_only=True, access=access, refresh=refresh)
                flow.run_protocol_login(provider, 'user@example.com', 'password')
                flow.follow_redirect_chain.assert_called_once()
                flow.fetch_client_auth_session_dump.assert_called_once()

    def test_web_session_mode_still_consumes_its_flow(self):
        flow, provider = self.make_flow(refresh_only=False, access='access', refresh='refresh')
        flow.run_protocol_login(provider, 'user@example.com', 'password')
        flow.follow_redirect_chain.assert_called_once()
        flow.get_auth_session.assert_called()
