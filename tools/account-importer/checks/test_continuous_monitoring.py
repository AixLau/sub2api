import asyncio
import unittest
from datetime import timedelta
from unittest.mock import patch

from fake_store import FakeStore, wait_for_operation
from job_manager import Job, JobManager, MAX_RELOGIN_ATTEMPTS, _now
from sub2api_client import AdminAPIError
from test_web_service import FakeClient, bundle

LINE = "account@example.com----original-password----JBSWY3DPEHPK3PXP"


async def until(predicate):
    async with asyncio.timeout(3):
        while not predicate():
            await asyncio.sleep(0.003)


class ContinuousMonitoringTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.client, self.store = FakeClient(), FakeStore()
        await self.client.import_data(bundle())
        self.record = self.client.records['account@example.com']
        self.logins, self.polls = [], 0
        async def get(account_id):
            self.polls += 1
            return dict(self.record)
        self.client.get_account = get
        def fail(line, fmt, **kwargs):
            self.logins.append(line)
            raise RuntimeError('original-password must remain private')
        self.manager = JobManager(self.client, store=self.store, login=fail, poll_seconds=0.003, retry_seconds=0.005)
        self.job = Job(name='account@example.com', mode='credentials', group_ids=[2], account_id=1, credential_line=LINE)
        self.manager.jobs[self.job.id] = self.job

    async def asyncTearDown(self):
        await self.manager.close()

    async def test_any_account_error_has_initial_attempt_and_three_retries_then_keeps_polling(self):
        self.record.update(status='error', error_message='generic upstream failure')
        await self.manager._start_watch(self.job)
        await until(lambda: len(self.logins) == MAX_RELOGIN_ATTEMPTS)
        polls = self.polls
        await until(lambda: self.polls >= polls + 5)
        self.assertEqual(len(self.logins), 4)
        self.assertEqual(self.job.attempts, 4)
        self.assertIn('3 次重试', self.job.message)
        self.assertTrue(self.job.public()['monitoring_enabled'])
        self.assertFalse(self.job.task.done())
        self.assertNotIn('original-password', str(self.job.public()))

    async def test_confirmed_recovery_resets_budget_for_next_incident(self):
        self.record.update(status='error', error_message='failure')
        await self.manager._start_watch(self.job)
        await until(lambda: self.job.attempts == 4 and self.job.state == 'invalid')
        self.record.update(status='active', error_message='')
        await until(lambda: self.job.attempts == 0)
        self.assertIsNone(self.job.next_relogin)
        self.record.update(status='error', error_message='new failure')
        await until(lambda: len(self.logins) == 8)
        self.assertEqual(self.job.attempts, 4)

    async def test_success_waits_for_healthy_poll_and_uses_original_credentials(self):
        def success(line, fmt, **kwargs):
            self.logins.append(line)
            return bundle(token='new-token')
        self.manager.login = success
        self.record.update(status='error', error_message='401')
        await self.manager._start_watch(self.job)
        await until(lambda: len(self.logins) == 1 and self.job.attempts == 0)
        self.assertEqual(self.logins, [LINE])
        self.assertEqual(self.job.state, 'monitoring')
        self.assertEqual(self.record['credentials']['model_mapping'], {'gpt-5.5':'gpt-5.5'})

    async def test_json_errors_remain_monitored_and_recover_without_login(self):
        self.job.mode, self.job.credential_line = 'file', None
        self.record.update(status='error', error_message='401')
        await self.manager._start_watch(self.job)
        await until(lambda: self.polls >= 5)
        self.assertEqual(self.job.state, 'invalid')
        self.assertFalse(self.job.task.done())
        self.record.update(status='active', error_message='')
        await until(lambda: self.job.state == 'monitoring')
        self.assertEqual(self.logins, [])

    async def test_status_query_failure_does_not_authorize(self):
        async def unavailable(account_id):
            self.polls += 1
            raise AdminAPIError('unavailable', status_code=502)
        self.client.get_account = unavailable
        await self.manager._start_watch(self.job)
        await until(lambda: self.polls >= 4)
        self.assertEqual(self.job.state, 'attention')
        self.assertEqual(self.logins, [])
        self.assertFalse(self.job.task.done())

    async def test_deleted_account_stops_and_cannot_be_reauthorized(self):
        async def deleted(account_id):
            raise AdminAPIError('missing', status_code=404)
        self.client.get_account = deleted
        await self.manager._start_watch(self.job)
        await asyncio.wait_for(self.job.task, 1)
        self.assertEqual(self.job.state, 'removed')
        self.assertFalse(self.job.public()['monitoring_enabled'])
        with self.assertRaises(ValueError):
            await self.manager.reauthorize_credentials(self.job.id)
        self.assertEqual(self.logins, [])

    async def test_monitor_does_not_expire_when_clock_advances(self):
        await self.manager._start_watch(self.job)
        future = _now() + timedelta(days=2)
        with patch('job_manager._now', return_value=future):
            await until(lambda: self.polls >= 5)
        self.assertFalse(self.job.task.done())
        self.assertEqual(self.job.state, 'monitoring')
        self.assertEqual(self.job.last_checked_at, future)
        self.assertNotIn('watch_until', self.job.public())

    async def test_restart_preserves_exhaustion_and_resumes_monitoring(self):
        self.record.update(status='error', error_message='failure')
        self.job.state, self.job.attempts = 'invalid', 4
        self.job.next_relogin = _now()+timedelta(seconds=60)
        await self.store.save(self.job.stored())
        self.manager.jobs.clear()
        await self.manager.start()
        restored = self.manager.get_job(self.job.id)
        await until(lambda: self.polls >= 4)
        self.assertEqual(restored.attempts, 4)
        self.assertEqual(self.logins, [])
        self.assertFalse(restored.task.done())

    async def test_restart_preserves_retry_cooldown(self):
        self.record.update(status='error', error_message='failure')
        self.job.state, self.job.attempts = 'invalid', 2
        self.job.next_relogin = _now()+timedelta(seconds=60)
        await self.store.save(self.job.stored())
        self.manager.jobs.clear()
        await self.manager.start()
        await until(lambda: self.polls >= 4)
        self.assertEqual(self.manager.get_job(self.job.id).attempts, 2)
        self.assertEqual(self.logins, [])

    async def test_failed_manual_authorization_resumes_monitoring(self):
        self.record.update(status='error', error_message='failure')
        self.job.state, self.job.attempts = 'invalid', 4
        await self.manager.reauthorize_credentials(self.job.id)
        await wait_for_operation(self.job)
        await until(lambda: self.polls >= 3)
        self.assertEqual(self.logins, [LINE])
        self.assertFalse(self.job.task.done())

    async def test_disabled_account_is_not_automatically_reenabled(self):
        self.record.update(status='inactive', error_message='')
        await self.manager._start_watch(self.job)
        await until(lambda: self.polls >= 3)
        self.assertEqual(self.logins, [])
        self.assertEqual(self.record['status'], 'inactive')


    async def test_missing_models_are_visible_and_still_monitored(self):
        self.record['credentials'].pop('model_mapping')
        await self.manager._start_watch(self.job)
        await until(lambda: self.polls >= 3)
        self.assertEqual(self.job.state, 'attention')
        self.assertIn('未设置模型限制', self.job.message)
        self.assertFalse(self.job.task.done())
        self.assertEqual(self.logins, [])

    async def test_failed_reimport_keeps_known_account_monitored(self):
        async def fail_models(account_id, settings):
            raise AdminAPIError('write failed')
        self.client.set_model_restrictions = fail_models
        result = await self.manager.import_file(bundle(), profile_account_id=285)
        imported = self.manager.get_job(result[0]['id'])
        await until(lambda: self.polls >= 3)
        self.assertFalse(imported.task.done())
        self.assertTrue(imported.public()['monitoring_enabled'])


    async def test_empty_pool_automatic_reauthorization_is_direct_and_clears_old_proxy(self):
        calls = []
        async def no_proxies():
            return []
        def success(line, fmt, **kwargs):
            calls.append((line, kwargs["proxy"]))
            return bundle(token="direct-token")
        self.client.list_active_proxies = no_proxies
        self.manager.login = success
        self.job.proxy_id, self.job.login_proxy = 999, "http://old-proxy:8080"
        self.record.update(proxy_id=999, status="error", error_message="401")
        await self.manager._start_watch(self.job)
        await until(lambda: calls and self.job.attempts == 0)
        self.assertEqual(calls, [(LINE, None)])
        self.assertEqual(self.record["proxy_id"], 0)
        self.assertIsNone(self.job.proxy_id)
        self.assertIsNone(self.job.login_proxy)
        self.assertFalse(self.job.task.done())
