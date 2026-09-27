from fake_store import FakeStore, wait_for_operation
import asyncio
from copy import deepcopy
import json
import re
import unittest

import httpx

from job_manager import JobManager
from sub2api_client import AdminAPIError, Sub2APIClient, plugin_account_ids
from test_web_service import FakeClient, bundle
from web_service import create_app


def plugin(plugin_id=7, state='enabled', healthy=True, ids=None):
    return {'id': plugin_id, 'name': '测试插件', 'version': '1.0',
            'state': state, 'runtime_healthy': healthy, 'bindings': [{
                'enabled': True, 'platform': 'openai', 'account_type': 'oauth',
                'capability': 'openai.oauth.outbound_transport.v1',
                'account_ids': [285] if ids is None else ids}]}


class PluginClient(FakeClient):
    list_enabled_plugins = Sub2APIClient.list_enabled_plugins
    get_enabled_plugin = Sub2APIClient.get_enabled_plugin
    append_plugin_accounts = Sub2APIClient.append_plugin_accounts

    def __init__(self):
        super().__init__()
        self.plugins = [plugin(), plugin(8, 'disabled'), plugin(9, healthy=False)]
        self.updates = []
        self.fail_update = False

    async def _request(self, method, path, **kwargs):
        if path == 'plugins':
            return deepcopy(self.plugins)
        current = next(item for item in self.plugins if item['id'] == int(path.split('/')[1]))
        if method == 'GET':
            return deepcopy(current)
        if self.fail_update:
            raise AdminAPIError('update failed')
        self.updates.append(deepcopy(kwargs['payload']))
        current['bindings'][0]['account_ids'] = kwargs['payload']['account_ids']
        return deepcopy(current)


class PluginImportTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.client = PluginClient()
        self.manager = JobManager(self.client, store=FakeStore(), poll_seconds=0.01)

    async def asyncTearDown(self):
        await self.manager.close()

    async def test_only_enabled_healthy_compatible_plugins(self):
        unsupported = plugin(10)
        unsupported['bindings'][0]['platform'] = 'anthropic'
        self.client.plugins.append(unsupported)
        self.assertEqual([item['id'] for item in await self.client.list_enabled_plugins()], [7])

    async def test_merge_keeps_existing_accounts_and_deduplicates(self):
        await self.client.append_plugin_accounts(7, [2, 2, 1])
        self.assertEqual(self.client.updates, [{'account_ids': [1, 2, 285], 'accept_untested': False}])
        await self.client.append_plugin_accounts(7, [1])
        self.assertEqual(len(self.client.updates), 1)

    async def test_malformed_scope_is_rejected(self):
        self.client.plugins[0]['bindings'][0]['account_ids'] = None
        with self.assertRaises(AdminAPIError):
            await self.client.append_plugin_accounts(7, [1])
        self.assertEqual(self.client.updates, [])

    async def test_invalid_or_disabled_selection_stops_before_import(self):
        for selected in (0, -1, True, 8, 9):
            with self.subTest(selected=selected), self.assertRaises(ValueError):
                await self.manager.import_file(bundle(), profile_account_id=285, plugin_id=selected)
        self.assertEqual(self.client.records, {})
        self.assertEqual(self.manager.jobs, {})

    async def test_import_and_reupload_bind_once_and_keep_settings(self):
        first = (await self.manager.import_file(bundle(), profile_account_id=285, plugin_id=7))[0]
        self.assertEqual(first['plugin_status'], 'bound')
        self.assertEqual(self.client.updates[0]['account_ids'], [1, 285])
        second = (await self.manager.import_file(bundle(token='changed'), plugin_id=7))[0]
        self.assertEqual(second['plugin_status'], 'bound')
        self.assertEqual(len(self.client.records), 1)
        self.assertEqual(len(self.client.updates), 1)
        self.assertEqual(self.client.records['account@example.com']['group_ids'], [2, 15])

    async def test_batch_binds_only_successful_accounts(self):
        original = self.client.import_data
        async def partial(payload):
            reduced = deepcopy(payload)
            reduced['accounts'] = reduced['accounts'][:1]
            result = await original(reduced)
            result['account_failed'] = 1
            return result
        self.client.import_data = partial
        payload = bundle()
        payload['accounts'] += bundle('second@example.com')['accounts']
        jobs = await self.manager.import_file(payload, profile_account_id=285, plugin_id=7)
        self.assertEqual([job['plugin_status'] for job in jobs], ['bound', 'skipped'])
        self.assertEqual(self.client.updates[0]['account_ids'], [1, 285])

    async def test_binding_failure_remains_visible_after_monitor_poll(self):
        self.client.fail_update = True
        job = (await self.manager.import_file(bundle(), profile_account_id=285, plugin_id=7))[0]
        await asyncio.sleep(0.04)
        current = self.manager.get_job(job['id']).public()
        self.assertEqual(current['plugin_status'], 'failed')
        self.assertIn('账号已导入', current['plugin_message'])
        self.assertEqual(current['state'], 'monitoring')
        self.assertEqual(len(self.client.records), 1)

    async def test_disabled_during_import_is_not_reenabled(self):
        original = self.client.import_data
        async def disable(payload):
            result = await original(payload)
            self.client.plugins[0]['state'] = 'disabled'
            return result
        self.client.import_data = disable
        jobs = await self.manager.import_file(bundle(), profile_account_id=285, plugin_id=7)
        self.assertEqual(jobs[0]['plugin_status'], 'failed')
        self.assertEqual(self.client.updates, [])

    async def test_new_binding_added_during_import_is_preserved(self):
        original = self.client.import_data
        async def another_binding(payload):
            result = await original(payload)
            self.client.plugins[0]['bindings'][0]['account_ids'].append(286)
            return result
        self.client.import_data = another_binding
        await self.manager.import_file(bundle(), profile_account_id=285, plugin_id=7)
        self.assertEqual(self.client.updates[0]['account_ids'], [1, 285, 286])

    async def test_no_plugin_selection_makes_no_binding_request(self):
        jobs = await self.manager.import_file(bundle(), profile_account_id=285)
        self.assertIsNone(jobs[0]['plugin_id'])
        self.assertEqual(self.client.updates, [])

    async def test_http_upload_forwards_plugin_and_requires_csrf(self):
        app = create_app(self.client, store=FakeStore(), poll_seconds=0.01)
        try:
            async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url='http://localhost') as http:
                self.assertEqual([item['id'] for item in (await http.get('/api/plugins')).json()], [7])
                page = await http.get('/')
                self.assertIn('file-plugin', page.text)
                csrf = re.search(r'name="csrf-token" content="([^"]+)"', page.text).group(1)
                args = {'files': {'file': ('input.json', json.dumps(bundle()).encode(), 'application/json')},
                        'data': {'profile_account_id': '285', 'plugin_id': '7'}}
                self.assertEqual((await http.post('/api/import/file', **args)).status_code, 403)
                response = await http.post('/api/import/file', headers={'X-CSRF-Token': csrf}, **args)
                self.assertEqual(response.status_code, 202, response.text)
                self.assertEqual(response.json()[0]['plugin_status'], 'bound')
        finally:
            await app.state.manager.close()

    async def test_plugin_list_failure_is_reported(self):
        async def fail():
            raise AdminAPIError('denied')
        self.client.list_enabled_plugins = fail
        app = create_app(self.client, store=FakeStore())
        try:
            async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url='http://localhost') as http:
                self.assertEqual((await http.get('/api/plugins')).status_code, 502)
        finally:
            await app.state.manager.close()

    async def test_real_http_client_uses_documented_paths_and_payload(self):
        requests = []
        async def handler(request):
            requests.append(request)
            record = plugin()
            if request.method == 'POST':
                record['bindings'][0]['account_ids'] = json.loads(request.content)['account_ids']
            return httpx.Response(200, json={'code': 0, 'data': record})
        client = Sub2APIClient('https://example.test', 'test-only', transport=httpx.MockTransport(handler))
        try:
            await client.append_plugin_accounts(7, [1])
            self.assertEqual([r.url.path for r in requests], ['/api/v1/admin/plugins/7', '/api/v1/admin/plugins/7/enable'])
            self.assertEqual(json.loads(requests[1].content), {'account_ids': [1, 285], 'accept_untested': False})
        finally:
            await client.close()


class PluginCredentialTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.client = PluginClient()
        self.login_calls = []
        def login(line, fmt, **kwargs):
            self.login_calls.append((line, fmt, kwargs))
            return bundle()
        self.manager = JobManager(self.client, store=FakeStore(), login=login, poll_seconds=0.01, retry_seconds=0)
        self.line = "account@example.com----test-password----JBSWY3DPEHPK3PXP"

    async def asyncTearDown(self):
        await self.manager.close()

    async def import_account(self, plugin_id=7):
        submitted = await self.manager.import_credentials(self.line, profile_account_id=285, plugin_id=plugin_id)
        job = self.manager.get_job(submitted['id'])
        await wait_for_operation(job)
        return job

    async def test_credentials_new_account_binds_after_login_and_import(self):
        job = await self.import_account()
        self.assertEqual((job.account_id, job.plugin_status, job.state), (1, 'bound', 'monitoring'))
        self.assertEqual(self.client.updates[0]['account_ids'], [1, 285])
        self.assertEqual(len(self.login_calls), 1)
        public = json.dumps(job.public())
        self.assertNotIn('test-password', public)
        self.assertNotIn('JBSWY3DPEHPK3PXP', public)

    async def test_credentials_existing_account_and_auto_relogin_preserve_binding(self):
        await self.client.import_data(bundle())
        job = await self.import_account()
        self.assertEqual(job.plugin_status, 'bound')
        self.assertEqual(len(self.client.records), 1)
        self.assertEqual(len(self.client.updates), 1)
        await self.manager._retry_relogin(job)
        self.assertEqual(len(self.client.updates), 1)
        self.assertEqual(job.plugin_status, 'bound')
        self.assertEqual(len(self.login_calls), 2)

    async def test_invalid_plugin_rejected_before_login(self):
        with self.assertRaises(ValueError):
            await self.manager.import_credentials(self.line, profile_account_id=285, plugin_id=8)
        self.assertEqual(self.login_calls, [])
        self.assertEqual(self.manager.jobs, {})

    async def test_credentials_login_failure_skips_plugin_and_preserves_retry_credentials(self):
        def fail(*args, **kwargs):
            raise RuntimeError('test-password')
        self.manager.login = fail
        job = await self.import_account()
        self.assertEqual((job.state, job.plugin_status), ('failed', 'skipped'))
        self.assertEqual(self.client.updates, [])
        self.assertEqual(self.client.records, {})
        self.assertIsNotNone(job.credential_line)
        self.assertNotIn('test-password', json.dumps(job.public()))

    async def test_credentials_binding_failure_does_not_undo_import(self):
        self.client.fail_update = True
        job = await self.import_account()
        await asyncio.sleep(0.03)
        self.assertEqual((job.state, job.plugin_status), ('monitoring', 'failed'))
        self.assertIn('账号已导入', job.plugin_message)
        self.assertEqual(len(self.client.records), 1)

    async def test_credentials_no_selection_skips_plugin(self):
        job = await self.import_account(plugin_id=None)
        self.assertEqual(job.state, 'monitoring')
        self.assertIsNone(job.plugin_id)
        self.assertEqual(self.client.updates, [])

    async def test_credentials_plugin_disabled_during_login_is_not_enabled(self):
        def login(*args, **kwargs):
            self.client.plugins[0]['state'] = 'disabled'
            return bundle()
        self.manager.login = login
        job = await self.import_account()
        self.assertEqual((job.state, job.plugin_status), ('monitoring', 'failed'))
        self.assertEqual(self.client.updates, [])

    async def test_credentials_http_forwards_selection_and_rejects_invalid_id(self):
        app = create_app(self.client, store=FakeStore(), login=self.manager.login, poll_seconds=0.01)
        try:
            async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url='http://localhost') as http:
                page = await http.get('/')
                csrf = re.search(r'name="csrf-token" content="([^"]+)"', page.text).group(1)
                data = {'account_line': self.line, 'group_ids': [], 'plugin_id': 7}
                response = await http.post('/api/import/account?profile_account_id=285', json=data, headers={'X-CSRF-Token': csrf})
                self.assertEqual(response.status_code, 202, response.text)
                job = app.state.manager.get_job(response.json()['id'])
                await wait_for_operation(job)
                self.assertEqual(job.plugin_status, 'bound')
                for invalid in (0, -1, True):
                    data['plugin_id'] = invalid
                    response = await http.post('/api/import/account?profile_account_id=285', json=data, headers={'X-CSRF-Token': csrf})
                    self.assertEqual(response.status_code, 422)
                    self.assertNotIn('test-password', response.text)
        finally:
            await app.state.manager.close()


if __name__ == '__main__':
    unittest.main()
