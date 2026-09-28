import json
import unittest
from copy import deepcopy

import httpx

from fake_store import FakeStore, wait_for_operation
from job_manager import JobManager
from sub2api_client import Sub2APIClient, AdminAPIError
from test_web_service import FakeClient, bundle
from web_service import create_app

LINE = "account@example.com----original-password----JBSWY3DPEHPK3PXP"
MODELS = {'model_mapping':{'model-a':'model-a','model-b':'upstream-b'},'compact_model_mapping':{'model-b':'compact-b'}}


class ModelRestrictionTests(unittest.IsolatedAsyncioTestCase):
    async def test_both_modes_reject_empty_reference_before_login_or_import(self):
        for mode in ('file','credentials'):
            client, calls = FakeClient(), []
            client.reference['credentials'] = {}
            manager = JobManager(client, store=FakeStore(), login=lambda *a, **kw:calls.append(1))
            try:
                with self.assertRaisesRegex(ValueError, '未设置模型限制'):
                    if mode == 'file':
                        await manager.import_file(bundle(), profile_account_id=285)
                    else:
                        await manager.import_credentials(LINE, profile_account_id=285)
                self.assertFalse(calls)
                self.assertFalse(client.records)
                self.assertFalse(manager.jobs)
            finally:
                await manager.close()

    async def test_import_applies_and_verifies_models_even_if_import_endpoint_omits_them(self):
        client = FakeClient()
        client.reference['credentials'] = deepcopy(MODELS)
        original = client.import_data
        async def omit(payload):
            result = await original(payload)
            for record in client.records.values():
                record['credentials'].pop('model_mapping',None)
                record['credentials'].pop('compact_model_mapping',None)
            return result
        client.import_data = omit
        manager = JobManager(client, store=FakeStore())
        try:
            result = await manager.import_file(bundle(), profile_account_id=285)
            self.assertEqual(result[0]['state'], 'monitoring')
            for key,value in MODELS.items():
                self.assertEqual(client.records['account@example.com']['credentials'][key], value)
            self.assertEqual(result[0]['model_restrictions'], MODELS)
        finally:
            await manager.close()

    async def test_reimport_applies_selected_reference_models_for_both_modes(self):
        for mode in ('file','credentials'):
            client = FakeClient()
            await client.import_data(bundle())
            client.reference['credentials'] = deepcopy(MODELS)
            manager = JobManager(client, store=FakeStore(), login=lambda *a,**kw:bundle())
            try:
                if mode=='file':
                    await manager.import_file(bundle(),profile_account_id=285)
                else:
                    job=await manager.import_credentials(LINE,profile_account_id=285)
                    await wait_for_operation(manager.get_job(job['id']))
                for key,value in MODELS.items():
                    self.assertEqual(client.records['account@example.com']['credentials'][key],value)
            finally:
                await manager.close()

    async def test_preview_exposes_models_without_credentials(self):
        client=FakeClient();client.reference['credentials']={**deepcopy(MODELS),'refresh_token':'private-reference-token'}
        app=create_app(client,store=FakeStore())
        try:
            async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app),base_url='http://localhost') as http:
                response=await http.get('/api/reference/285/models')
                self.assertEqual(response.status_code,200)
                self.assertEqual(response.json()['model_mapping'],MODELS['model_mapping'])
                self.assertNotIn('private-reference-token',response.text)
                client.reference['credentials']={}
                self.assertEqual((await http.get('/api/reference/285/models')).status_code,400)
        finally:
            await app.state.manager.close()

    async def test_admin_update_preserves_identity_and_checks_persisted_values(self):
        state={'id':1,'credentials':{'email':'account@example.com','client_id':'existing','model_mapping':{'old':'old'}}}
        writes=[]
        def respond(request):
            if request.method=='PUT':
                value=json.loads(request.content);writes.append(value)
                state['credentials']=value['credentials']
            return httpx.Response(200,json={'code':0,'data':deepcopy(state)})
        client=Sub2APIClient('https://example.test','test',httpx.MockTransport(respond))
        try:
            await client.set_model_restrictions(1,MODELS)
            self.assertEqual(writes[0]['credentials']['email'],'account@example.com')
            self.assertEqual(writes[0]['credentials']['client_id'],'existing')
            self.assertEqual(writes[0]['credentials']['model_mapping'],MODELS['model_mapping'])
            await client.set_model_restrictions(1,MODELS)
            self.assertEqual(len(writes),1)
        finally:
            await client.close()

    async def test_unpersisted_models_fail_verification(self):
        client=Sub2APIClient('https://example.test','test',httpx.MockTransport(lambda r:httpx.Response(200,json={'code':0,'data':{'id':1,'credentials':{}}})))
        try:
            with self.assertRaisesRegex(AdminAPIError,'校验失败'):
                await client.set_model_restrictions(1,MODELS)
        finally:
            await client.close()

    async def test_authorization_does_not_accept_model_overrides_from_token_file(self):
        writes=[]
        def respond(request):
            if request.method != 'GET':
                writes.append(json.loads(request.content))
            return httpx.Response(200,json={'code':0,'data':{'id':1,'status':'active','schedulable':True}})
        client=Sub2APIClient('https://example.test','test',httpx.MockTransport(respond))
        try:
            document=bundle()['accounts'][0]
            document['credentials'].update(MODELS)
            await client.apply_oauth(1,document)
            self.assertNotIn('model_mapping',writes[0]['credentials'])
            self.assertNotIn('compact_model_mapping',writes[0]['credentials'])
            self.assertEqual(writes[0]['credentials']['access_token'],'token-one')
        finally:
            await client.close()
