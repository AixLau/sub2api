import asyncio
import json
import os
import unittest
from datetime import timedelta
from unittest.mock import patch

from cryptography.fernet import Fernet
from redis.asyncio import Redis
from redis.exceptions import ConnectionError

from fake_store import FakeStore
from job_manager import Job, JobManager, _now
from job_store import KEY, RedisJobStore, StorageError
from test_web_service import FakeClient, bundle

LINE = 'account@example.com----original-password----JBSWY3DPEHPK3PXP'


class RedisStub:
    def __init__(self):
        self.data = {}
    async def ping(self):
        return True
    async def hset(self, key, field, value):
        self.data.setdefault(key, {})[field.encode()] = value
    async def hgetall(self, key):
        return self.data.get(key, {}).copy()
    async def aclose(self):
        pass


class JobStoreChecks(unittest.IsolatedAsyncioTestCase):
    async def test_records_encrypted_and_wrong_key_fails_closed(self):
        redis = RedisStub()
        key = Fernet.generate_key()
        store = RedisJobStore(redis, key)
        job = Job(name='account@example.com', mode='credentials', group_ids=[2], credential_line=LINE)
        await store.save(job.stored())
        raw = next(iter(redis.data[KEY].values()))
        self.assertNotIn(b'original-password', raw)
        self.assertNotIn(b'JBSWY3DPEHPK3PXP', raw)
        self.assertNotIn(b'account@example.com', raw)
        self.assertEqual((await store.load())[0]['credential_line'], LINE)
        with self.assertRaises(StorageError):
            await RedisJobStore(redis, Fernet.generate_key()).load()
        self.assertEqual((await store.load())[0]['credential_line'], LINE)

    async def test_storage_failure_does_not_start_login_or_accept_import(self):
        class FailedStore(FakeStore):
            async def save(self, record):
                raise StorageError('Redis unavailable')
        calls = []
        manager = JobManager(FakeClient(), store=FailedStore(), login=lambda *a, **kw: calls.append(1))
        with self.assertRaises(StorageError):
            await manager.import_credentials(LINE, profile_account_id=285)
        self.assertFalse(calls)
        self.assertFalse(manager.jobs)
        await manager.close()

    async def test_restart_recovers_original_credentials_and_allows_one_click(self):
        store = FakeStore()
        client = FakeClient()
        await client.import_data(bundle())
        calls = []
        def login(line, fmt, **kwargs):
            calls.append(line)
            return bundle(token='restart-access')
        first = JobManager(client, store=store, login=login)
        job = Job(name='account@example.com', mode='credentials', group_ids=[2], account_id=1,
                  state='completed', credential_line=LINE, deadline=_now()-timedelta(seconds=1))
        first.jobs[job.id] = job
        await first.close()
        self.assertIsNone(job.credential_line)
        second = JobManager(client, store=store, login=login)
        await second.start()
        restored = second.get_job(job.id)
        self.assertEqual(restored.credential_line, LINE)
        self.assertNotIn('original-password', json.dumps(second.list_jobs()))
        await second.reauthorize_credentials(job.id)
        await restored.task
        self.assertEqual(calls, [LINE])
        self.assertEqual(client.applied, [(1, 'restart-access')])
        await second.close()

    async def test_interrupted_first_import_retries_using_saved_profile(self):
        store, client = FakeStore(), FakeClient()
        job = Job(name='account@example.com', mode='credentials', group_ids=[], credential_line=LINE, profile_account_id=285)
        await store.save(job.stored())
        manager = JobManager(client, store=store, login=lambda *a, **kw: bundle())
        await manager.start()
        restored = manager.get_job(job.id)
        self.assertEqual(restored.state, 'attention')
        await manager.reauthorize_credentials(job.id)
        await restored.task
        self.assertEqual(restored.account_id, 1)
        self.assertFalse(restored.manual_pending)
        await manager.close()

    async def test_existing_monitor_deadline_is_not_extended_on_restart(self):
        store, client = FakeStore(), FakeClient()
        await client.import_data(bundle())
        deadline = _now()+timedelta(seconds=300)
        job = Job(name='account@example.com', mode='file', group_ids=[2], account_id=1, state='monitoring', deadline=deadline)
        await store.save(job.stored())
        manager = JobManager(client, store=store)
        await manager.start()
        self.assertEqual(manager.get_job(job.id).deadline, deadline)
        self.assertIsNotNone(manager.get_job(job.id).task)
        await manager.close()

    async def test_same_database_is_rejected_before_connection(self):
        with patch.dict(os.environ, {'ACCOUNT_IMPORT_REDIS_DB':'0', 'SUB2API_REDIS_DB':'0'}):
            with self.assertRaisesRegex(ValueError, '不同'):
                RedisJobStore.from_env()

    async def test_redis_connection_errors_do_not_leak_credentials(self):
        class Offline(RedisStub):
            async def hset(self, *args):
                raise ConnectionError('secret-connection-string')
        store = RedisJobStore(Offline(), Fernet.generate_key())
        with self.assertRaises(StorageError) as error:
            await store.save({'id':'offline', 'credential_line':LINE})
        self.assertNotIn('secret', str(error.exception))


@unittest.skipUnless(os.getenv('IMPORTER_REDIS_TEST_PORT'), 'Requires isolated local Redis test container')
class LiveRedisChecks(unittest.IsolatedAsyncioTestCase):
    async def test_database_isolation_and_reconnect_restore(self):
        port = int(os.environ['IMPORTER_REDIS_TEST_PORT'])
        db0, db1 = Redis(host='127.0.0.1',port=port,db=0), Redis(host='127.0.0.1',port=port,db=1)
        job = Job(name='redis-check@example.com',mode='credentials',group_ids=[],credential_line=LINE,state='completed')
        sentinel, test_key = 'gateway-test:'+job.id, KEY+':test:'+job.id
        key = Fernet.generate_key()
        store = RedisJobStore(db1,key)
        restored = RedisJobStore(Redis(host='127.0.0.1',port=port,db=1),key)
        with patch('job_store.KEY',test_key):
            try:
                await db0.set(sentinel,'unchanged')
                before = await db0.dbsize()
                await store.save(job.stored())
                self.assertEqual(await db0.dbsize(),before)
                self.assertEqual(await db0.get(sentinel),b'unchanged')
                self.assertFalse(await db0.exists(test_key))
                self.assertNotIn(b'original-password',await db1.hget(test_key,job.id))
                await store.close()
                self.assertTrue(any(r['id']==job.id and r['credential_line']==LINE for r in await restored.load()))
            finally:
                await db1.delete(test_key)
                await db0.delete(sentinel)
                await store.close()
                await restored.close()
                await db0.aclose()
