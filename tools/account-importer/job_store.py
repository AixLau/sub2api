"""Encrypted importer records in a separate database of the shared Redis."""
from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Protocol

from cryptography.fernet import Fernet, InvalidToken
from redis.asyncio import Redis
from redis.exceptions import RedisError

KEY = "sub2api-account-importer:v1:jobs"


class StorageError(RuntimeError):
    pass


class JobStore(Protocol):
    async def ping(self) -> None: ...
    async def save(self, record: dict) -> None: ...
    async def load(self) -> list[dict]: ...
    async def close(self) -> None: ...


class RedisJobStore:
    def __init__(self, client: Redis, encryption_key: bytes):
        self.client = client
        self.cipher = Fernet(encryption_key)

    @classmethod
    def from_env(cls) -> "RedisJobStore":
        db = int(os.environ["ACCOUNT_IMPORT_REDIS_DB"])
        gateway_db = int(os.environ["SUB2API_REDIS_DB"])
        if db < 0 or db == gateway_db:
            raise ValueError("导入服务必须使用与 Sub2API 不同的 Redis 数据库")
        password_file = os.getenv("ACCOUNT_IMPORT_REDIS_PASSWORD_FILE")
        password = Path(password_file).read_text().strip() if password_file else None
        key = Path(os.environ["ACCOUNT_IMPORT_ENCRYPTION_KEY_FILE"]).read_bytes().strip()
        return cls(Redis(
            host=os.environ["ACCOUNT_IMPORT_REDIS_HOST"],
            port=int(os.getenv("ACCOUNT_IMPORT_REDIS_PORT", "6379")),
            db=db, password=password or None,
            socket_connect_timeout=5, socket_timeout=5,
            decode_responses=False, protocol=2,
        ), key)

    async def ping(self) -> None:
        try:
            await self.client.ping()
        except RedisError:
            raise StorageError("Redis 不可用，未确认保存；请稍后重试") from None

    async def save(self, record: dict) -> None:
        raw = json.dumps(record, ensure_ascii=False, separators=(",", ":")).encode()
        encrypted = self.cipher.encrypt(raw)
        try:
            await self.client.hset(KEY, record["id"], encrypted)
        except RedisError:
            raise StorageError("Redis 保存失败，请稍后重试") from None

    async def load(self) -> list[dict]:
        try:
            records = await self.client.hgetall(KEY)
        except RedisError:
            raise StorageError("Redis 读取失败，无法恢复导入任务") from None
        result = []
        try:
            for field, encrypted in records.items():
                record = json.loads(self.cipher.decrypt(encrypted))
                if not isinstance(record, dict) or record.get("id") != field.decode():
                    raise ValueError("Invalid record")
                result.append(record)
        except (InvalidToken, ValueError, UnicodeError, TypeError):
            raise StorageError("导入任务解密或校验失败，请检查原加密密钥；未覆盖已有数据") from None
        return result

    async def close(self) -> None:
        await self.client.aclose()
