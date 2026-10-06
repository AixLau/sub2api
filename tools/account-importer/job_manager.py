"""Import OpenAI OAuth accounts and continuously watch their Sub2API state."""

from __future__ import annotations

import asyncio
from copy import deepcopy
import math
import secrets
from dataclasses import dataclass, field, fields
from datetime import datetime, timedelta, timezone
from typing import Callable

from default_profile import DEFAULT_PROFILE_ID, default_import_profile
from job_store import JobStore, StorageError
from import_payload import validate_bundle
from local_relogin import parse_account_line, relogin_payload
from model_restrictions import MODEL_SETTING_KEYS, model_settings
from proxy_pool import ProxyChoice, select_5x_proxy
from sub2api_client import AdminAPIError, Sub2APIClient

POLL_SECONDS = 30
RETRY_SECONDS = 60
MAX_RELOGIN_RETRIES = 3
MAX_RELOGIN_ATTEMPTS = 1 + MAX_RELOGIN_RETRIES
REFERENCE_CREDENTIAL_KEYS = MODEL_SETTING_KEYS
REFERENCE_EXTRA_KEYS = (
    "auto_reset_credit_enabled", "auto_reset_credit_5h_threshold",
    "auto_reset_credit_7d_threshold",
    "openai_long_context_billing_enabled",
    "openai_oauth_responses_websockets_v2_enabled",
    "openai_oauth_responses_websockets_v2_mode",
)
REFERENCE_SETTING_KEYS = (
    "concurrency", "priority", "rate_multiplier",
    "load_factor", "auto_pause_on_expired",
    "upstream_billing_probe_enabled", "upstream_billing_rate_sync_enabled",
)
AUTH_MARKERS = ("401", "403", "token", "oauth", "expired", "invalid", "授权", "凭证", "过期")


def _now() -> datetime:
    return datetime.now(timezone.utc)


def _parse_time(value: object) -> datetime | None:
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        try:
            return datetime.fromtimestamp(value, timezone.utc)
        except (ValueError, OverflowError, OSError):
            return None
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        return parsed if parsed.tzinfo else parsed.replace(tzinfo=timezone.utc)
    except ValueError:
        return None


def _account_state(account: dict) -> tuple[str, str]:
    status = str(account.get("status", "")).lower()
    message = str(account.get("error_message") or "")
    credentials = account.get("credentials_status") or {}
    if isinstance(credentials, dict):
        if credentials.get("has_access_token") is False or credentials.get("has_refresh_token") is False:
            return "invalid", "Sub2API 凭据缺少令牌"
    expires_at = _parse_time(account.get("expires_at"))
    if expires_at and expires_at <= _now():
        return "invalid", "Sub2API 账号已到期"
    if status == "error" and any(marker in message.lower() for marker in AUTH_MARKERS):
        return "invalid", "Sub2API 报告授权失效"
    if status == "error":
        return "attention", "Sub2API 报告账号错误"
    if status in {"disabled", "inactive"}:
        return "attention", "账号已暂停"
    return "monitoring", ""


def _seven_day_usage(usage: dict) -> dict:
    window = usage.get("seven_day") or {}
    used = window.get("utilization")
    if isinstance(used, bool) or not isinstance(used, (int, float)) or not math.isfinite(used) or not 0 <= used <= 100:
        used = None
    reset_at = _parse_time(window.get("resets_at"))
    updated_at = _parse_time(usage.get("updated_at"))
    cost = (window.get("window_stats") or {}).get("cost")
    if isinstance(cost, bool) or not isinstance(cost, (int, float)) or not math.isfinite(cost) or cost < 0:
        cost = None
    estimate = None
    if used is not None and used > 0 and cost is not None and cost > 0 and (reset_at is None or reset_at > _now()):
        candidate = cost / used * 100
        if math.isfinite(candidate):
            estimate = candidate
    return {
        "used_percent": used,
        "remaining_percent": 100 - used if used is not None else None,
        "reset_at": reset_at.isoformat() if reset_at else None,
        "updated_at": updated_at.isoformat() if updated_at else None,
        "account_cost": cost,
        "estimated_total_cost": estimate,
    }


def _account_profile(account: dict) -> dict:
    if account.get("platform") != "openai" or account.get("type") != "oauth":
        raise ValueError("参考账号不是 OpenAI OAuth 账号")
    bindings = account.get("account_groups") or []
    if bindings:
        group_ids = [int(item["group_id"]) for item in sorted(bindings, key=lambda item: item.get("priority", 0))]
    else:
        group_ids = [int(group_id) for group_id in account.get("group_ids") or []]
    if not group_ids:
        raise ValueError("参考账号未绑定分组")
    settings = {key: account[key] for key in REFERENCE_SETTING_KEYS if account.get(key) is not None}
    extra = account.get("extra") or {}
    credentials = account.get("credentials") or {}
    return {
        "group_ids": group_ids, "settings": settings,
        "extra": {key: deepcopy(extra[key]) for key in REFERENCE_EXTRA_KEYS if key in extra},
        "credentials": {key: deepcopy(credentials[key]) for key in REFERENCE_CREDENTIAL_KEYS if key in credentials},
        "account_id": int(account["id"]),
    }


def _prepared_new_account(account: dict, profile: dict, group_ids: list[int]) -> dict:
    prepared = deepcopy(account)
    prepared["group_ids"] = list(group_ids)
    for key in REFERENCE_SETTING_KEYS:
        if key in profile["settings"]:
            prepared[key] = profile["settings"][key]
        else:
            prepared.pop(key, None)
    for key in ("proxy_id", "proxy_key", "expires_at", "error_message"):
        prepared.pop(key, None)
    prepared.update(status="active", schedulable=True)
    credentials = prepared["credentials"]
    for key in REFERENCE_CREDENTIAL_KEYS:
        credentials.pop(key, None)
    credentials.update(model_settings(profile["credentials"]))
    extra = prepared.get("extra") or {}
    prepared["extra"] = {**{key: value for key, value in extra.items() if key not in REFERENCE_EXTRA_KEYS}, **profile["extra"]}
    return prepared


def _proxy_settings(proxy_id: int | None) -> dict:
    # Sub2API uses 0 to clear an existing proxy; null leaves it unchanged.
    return {"proxy_id": proxy_id if proxy_id is not None else 0}


def _import_proxy_settings(choice: ProxyChoice) -> dict:
    if choice.id is None:
        return {}
    return {"proxy_id": choice.id, "proxy_key": choice.key}


@dataclass
class Job:
    name: str
    mode: str
    group_ids: list[int]
    id: str = field(default_factory=lambda: secrets.token_urlsafe(12))
    state: str = "importing"
    account_id: int | None = None
    proxy_id: int | None = None
    plugin_id: int | None = None
    plugin_name: str = ""
    plugin_status: str = ""
    plugin_message: str = ""
    message: str = ""
    remote_status: str = ""
    current_concurrency: int | None = None
    last_used_at: str | None = None
    seven_day_usage: dict | None = None
    model_restrictions: dict | None = None
    schedulable: bool | None = None
    attempts: int = 0
    created_at: datetime = field(default_factory=_now)
    last_checked_at: datetime | None = None
    credential_line: str | None = field(default=None, repr=False)
    login_proxy: str | None = field(default=None, repr=False)
    next_relogin: datetime | None = field(default=None, repr=False)
    task: asyncio.Task | None = field(default=None, repr=False)
    manual_pending: bool = field(default=False, repr=False)
    retired: bool = False
    profile_account_id: int | None = None

    def stored(self) -> dict:
        record = {item.name: getattr(self, item.name) for item in fields(self)
                  if item.name not in {"task", "manual_pending"}}
        for key in ("created_at", "last_checked_at", "next_relogin"):
            value = record[key]
            record[key] = value.isoformat() if value else None
        return record

    @classmethod
    def restore(cls, record: dict) -> "Job":
        persisted = {item.name for item in fields(cls)} - {"task", "manual_pending"}
        values = {key: value for key, value in record.items() if key in persisted}
        for key in ("created_at", "last_checked_at", "next_relogin"):
            values[key] = _parse_time(values.get(key))
        if not values["created_at"]:
            raise ValueError("无效任务时间")
        return cls(**values)

    def public(self) -> dict:
        return {
            "id": self.id, "name": self.name, "mode": self.mode,
            "has_saved_credentials": bool(self.credential_line), "retired": self.retired,
            "state": self.state, "account_id": self.account_id,
            "proxy_id": self.proxy_id,
            "plugin_id": self.plugin_id, "plugin_name": self.plugin_name,
            "plugin_status": self.plugin_status, "plugin_message": self.plugin_message,
            "message": self.message, "remote_status": self.remote_status,
            "current_concurrency": self.current_concurrency,
            "last_used_at": self.last_used_at, "schedulable": self.schedulable,
            "seven_day_usage": self.seven_day_usage,
            "model_restrictions": self.model_restrictions,
            "attempts": self.attempts, "group_ids": self.group_ids,
            "created_at": self.created_at.isoformat(),
            "monitoring_enabled": bool(self.account_id and not self.retired and self.state != "removed"),
            "last_checked_at": self.last_checked_at.isoformat() if self.last_checked_at else None,
            "max_authorization_attempts": MAX_RELOGIN_ATTEMPTS,
        }


class JobManager:
    def __init__(
        self, client: Sub2APIClient, *, store: JobStore,
        poll_seconds: float = POLL_SECONDS, retry_seconds: int = RETRY_SECONDS,
        login: Callable = relogin_payload,
    ) -> None:
        self.client = client
        self.store = store
        self.poll_seconds = poll_seconds
        self.retry_seconds = retry_seconds
        self.login = login
        self.jobs: dict[str, Job] = {}
        self._import_lock = asyncio.Lock()
        self._login_lock = asyncio.Lock()
        self._closing = False

    async def start(self) -> None:
        await self.store.ping()
        try:
            restored = [Job.restore(record) for record in await self.store.load()]
        except (ValueError, TypeError, KeyError):
            raise StorageError("已保存任务结构无效，未覆盖数据") from None
        for job in sorted(restored, key=lambda item: item.created_at):
            self.jobs[job.id] = job
            if job.retired or job.state == "removed":
                continue
            if job.account_id:
                job.state, job.message = "monitoring", "持续监控已恢复"
                await self._save(job)
                self._resume_watch(job)
            elif job.state in {"importing", "reauthorizing"}:
                job.state, job.message = "attention", "服务重启前的导入未确认完成，请手动重新授权"
                await self._save(job)

    async def _save(self, job: Job) -> None:
        await self.store.save(job.stored())

    def list_jobs(self) -> list[dict]:
        return [job.public() for job in reversed(list(self.jobs.values()))]

    def get_job(self, job_id: str) -> Job:
        try:
            return self.jobs[job_id]
        except KeyError:
            raise ValueError("任务不存在") from None

    async def profile_accounts(self) -> list[dict]:
        return await self.client.list_openai_oauth_accounts()

    async def import_profile(self, account_id: int | None) -> dict:
        if account_id is None or account_id == DEFAULT_PROFILE_ID:
            return default_import_profile()
        if account_id < 0:
            raise ValueError("导入配置选项无效")
        profile = _account_profile(await self.client.get_profile_account(account_id))
        profile["credentials"] = model_settings(profile["credentials"])
        return profile

    async def watch_reference(self, account_id: int) -> dict:
        remote = await self.client.get_profile_account(account_id)
        profile = _account_profile(remote)
        account_id = int(remote["id"])
        for job in self.jobs.values():
            if job.mode == "reference" and job.account_id == account_id and job.task and not job.task.done():
                return job.public()
        job = Job(name=str(remote["name"]), mode="reference", group_ids=profile["group_ids"], account_id=account_id)
        await self._save(job)
        self.jobs[job.id] = job
        await self._start_watch(job)
        return job.public()

    async def import_file(self, payload: dict, override_group_ids: list[int] | None = None, profile_account_id: int | None = DEFAULT_PROFILE_ID, plugin_id: int | None = None) -> list[dict]:
        validate_bundle(payload)
        accounts = payload["accounts"]
        async with self._import_lock:
            plugin = await self.client.get_enabled_plugin(plugin_id) if plugin_id is not None else None
            existing = [await self.client.find_account(account["name"]) for account in accounts]
            profile = await self.import_profile(profile_account_id)
            models = [profile["credentials"] for _ in accounts]
            proxy_pool = await self.client.list_active_proxies()
            choices = [select_5x_proxy(proxy_pool) for _ in accounts]
            groups = [
                override_group_ids if override_group_ids is not None else
                (remote.get("group_ids") or [] if remote else profile["group_ids"])
                for item, remote in zip(accounts, existing)
            ]
            jobs = [
                Job(name=item["name"], mode="file", group_ids=list(group), proxy_id=choice.id, profile_account_id=profile_account_id)
                for item, group, choice in zip(accounts, groups, choices)
            ]
            for job in jobs:
                if plugin is not None:
                    job.plugin_id = plugin_id
                    job.plugin_name = str(plugin.get("name") or plugin_id)
                    job.plugin_status = "pending"
                await self._save(job)
                self.jobs[job.id] = job
            imported_jobs = []
            new_pairs = [(account, job, choice) for account, remote, job, choice in zip(accounts, existing, jobs, choices) if remote is None]
            if new_pairs:
                new_accounts = []
                for account, job, choice in new_pairs:
                    prepared = _prepared_new_account(account, profile, job.group_ids)
                    prepared.update(_import_proxy_settings(choice))
                    new_accounts.append(prepared)
                new_payload = {"proxies": [], "accounts": new_accounts}
                try:
                    result = await self.client.import_data(new_payload)
                except Exception:
                    for _, job, _ in new_pairs:
                        job.state, job.message = "failed", "导入请求失败；请在 Sub2API 核查后再重试"
                else:
                    for _, job, _ in new_pairs:
                        try:
                            remote = await self.client.find_account(job.name)
                            if not remote:
                                job.state, job.message = "failed", "服务器未创建此账号"
                                continue
                            job.account_id = int(remote["id"])
                            await self._apply_new_settings(job, profile)
                            imported_jobs.append(job)
                            await self._start_watch(job)
                        except Exception:
                            job.state, job.message = "attention", "账号可能已导入，但配置（含模型限制）未确认完成；请在 Sub2API 核查"
                    if result.get("account_failed"):
                        for _, job, _ in new_pairs:
                            if job.state == "monitoring":
                                job.message = "部分账号导入失败，请核查 Sub2API 导入结果"
            for account, remote, job, restrictions in zip(accounts, existing, jobs, models):
                if remote is None:
                    continue
                job.account_id = int(remote["id"])
                await self._retire_previous(job.account_id, job.id)
                try:
                    await self.client.apply_oauth(job.account_id, account)
                    await self.client.set_model_restrictions(job.account_id, restrictions)
                    job.model_restrictions = deepcopy(restrictions)
                    settings = {**_proxy_settings(job.proxy_id), "status": "active"}
                    if override_group_ids is not None:
                        settings["group_ids"] = job.group_ids
                    await self.client.update_settings(job.account_id, settings)
                    await self.client.set_schedulable(job.account_id, True)
                    imported_jobs.append(job)
                    await self._start_watch(job)
                except Exception:
                    job.state, job.message = "invalid", "重新授权失败，请核查文件和服务器"
            if plugin is not None:
                for job in jobs:
                    job.plugin_status = "skipped"
                    job.plugin_message = "账号导入或授权未完成，未加入插件"
                await self._bind_plugin_jobs(plugin_id, imported_jobs)
            for job in jobs:
                await self._save(job)
                self._resume_watch(job)
            return [job.public() for job in jobs]

    async def _bind_plugin_jobs(self, plugin_id: int | None, jobs: list[Job]) -> None:
        if plugin_id is None or not jobs:
            return
        try:
            await self.client.append_plugin_accounts(plugin_id, [job.account_id for job in jobs])
        except Exception:
            for job in jobs:
                job.plugin_status = "failed"
                job.plugin_message = "账号已导入，但插件绑定失败或结果未确认；请在 Sub2API 核查，也可重新提交导入重试"
        else:
            for job in jobs:
                job.plugin_status = "bound"
                job.plugin_message = "已加入插件"

    async def _retire_previous(self, account_id: int, new_job_id: str) -> None:
        for previous in self.jobs.values():
            if previous.id == new_job_id or previous.account_id != account_id:
                continue
            previous.retired = True
            if previous.task and not previous.task.done():
                previous.task.cancel()
                try:
                    await previous.task
                except asyncio.CancelledError:
                    pass
            previous.credential_line = None
            previous.login_proxy = None
            previous.state, previous.message = "completed", "已由新上传文件接管监控"
            await self._save(previous)

    async def _apply_new_settings(self, job: Job, profile: dict) -> None:
        await self.client.set_model_restrictions(job.account_id, profile["credentials"])
        job.model_restrictions = deepcopy(profile["credentials"])
        await self.client.update_settings(job.account_id, {**profile["settings"], "group_ids": job.group_ids, **_proxy_settings(job.proxy_id), "status": "active"})
        await self.client.set_schedulable(job.account_id, True)

    async def import_credentials(self, account_line: str, group_ids: list[int] | None = None, profile_account_id: int | None = DEFAULT_PROFILE_ID, plugin_id: int | None = None) -> dict:
        email, _, _ = parse_account_line(account_line)
        await self.import_profile(profile_account_id)
        if group_ids and any(isinstance(i, bool) or not isinstance(i, int) or i <= 0 for i in group_ids):
            raise ValueError("请选择有效分组")
        plugin = await self.client.get_enabled_plugin(plugin_id) if plugin_id is not None else None
        choice = select_5x_proxy(await self.client.list_active_proxies())
        job = Job(
            name=email, mode="credentials", group_ids=list(group_ids or []),
            profile_account_id=profile_account_id,
            credential_line=account_line,
            plugin_id=plugin_id,
            plugin_name=str(plugin.get("name") or plugin_id) if plugin is not None else "",
            plugin_status="pending" if plugin is not None else "",
            proxy_id=choice.id,
            login_proxy=choice.url,
        )
        await self._save(job)
        self.jobs[job.id] = job
        job.task = asyncio.create_task(self._login_and_import(job, profile_account_id, choice))
        return job.public()

    async def _login_and_import(self, job: Job, profile_account_id: int | None, choice: ProxyChoice) -> None:
        try:
            async with self._import_lock:
                remote = await self.client.find_account(job.name)
                profile = await self.import_profile(profile_account_id)
                restrictions = profile["credentials"]
                if not job.group_ids:
                    job.group_ids = list((remote.get("group_ids") or []) if remote else profile["group_ids"])
                async with self._login_lock:
                    payload = await asyncio.to_thread(
                        self.login, job.credential_line, "sub2api",
                        group_ids=job.group_ids, proxy=job.login_proxy,
                    )
                validate_bundle(payload)
                remote_name = payload["accounts"][0]["name"]
                if remote_name.casefold() != job.name.casefold():
                    raise ValueError("登录账号与提交账号不匹配")
                job.name = remote_name
                remote = await self.client.find_account(remote_name)
                if remote:
                    job.account_id = int(remote["id"])
                    await self._retire_previous(job.account_id, job.id)
                    await self.client.apply_oauth(job.account_id, payload["accounts"][0])
                    await self.client.set_model_restrictions(job.account_id, restrictions)
                    job.model_restrictions = deepcopy(restrictions)
                    settings = {**_proxy_settings(job.proxy_id), "status": "active"}
                    if job.group_ids and job.group_ids != (remote.get("group_ids") or []):
                        settings["group_ids"] = job.group_ids
                    await self.client.update_settings(job.account_id, settings)
                    await self.client.set_schedulable(job.account_id, True)
                    await self._bind_plugin_jobs(job.plugin_id, [job])
                    await self._start_watch(job)
                    return
                payload["accounts"][0] = _prepared_new_account(payload["accounts"][0], profile, job.group_ids)
                payload["accounts"][0].update(_import_proxy_settings(choice))
                payload["proxies"] = []
                await self.client.import_data(payload)
                remote = await self.client.find_account(remote_name)
                if not remote:
                    job.state, job.message = "failed", "服务器未创建此账号"
                    return
                job.account_id = int(remote["id"])
                await self._apply_new_settings(job, profile)
                await self._bind_plugin_jobs(job.plugin_id, [job])
                await self._start_watch(job)
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            # Keep the sanitized login-layer reason visible to the operator;
            # local_relogin removes the submitted password and TOTP seed before
            # raising. This is essential for distinguishing network/bootstrap
            # failures from bad credentials.
            reason = str(exc).strip()
            job.state, job.message = "failed", reason[:320] or "登录或导入失败；请核查账号、网络和 Sub2API 状态"
        finally:
            job.manual_pending = False
            if job.state == "failed" and job.plugin_id is not None:
                job.plugin_status = "skipped"
                job.plugin_message = "账号登录或导入未完成，未加入插件"
            await self._save(job)
            self._resume_watch(job)

    def _resume_watch(self, job: Job) -> None:
        if self._closing or not job.account_id or job.retired or job.state == "removed":
            return
        if job.task is None or job.task.done() or job.task is asyncio.current_task():
            job.task = asyncio.create_task(self._watch(job))

    async def _start_watch(self, job: Job) -> None:
        job.state, job.message = "monitoring", ""
        await self._save(job)
        self._resume_watch(job)

    async def _watch(self, job: Job) -> None:
        while not job.retired and not self._closing:
            try:
                account = await self.client.get_account(job.account_id)
                job.last_checked_at = _now()
                job.model_restrictions = model_settings(account.get("credentials"), required=False)
                job.remote_status = str(account.get("status") or "")
                job.current_concurrency = account.get("current_concurrency")
                job.last_used_at = account.get("last_used_at")
                job.schedulable = account.get("schedulable")
                try:
                    job.seven_day_usage = _seven_day_usage(await self.client.get_usage(job.account_id))
                except (AdminAPIError, ValueError, TypeError, AttributeError):
                    job.seven_day_usage = {"error": "用量与计费查询失败，等待重试"}
                state, message = _account_state(account)
                job.state, job.message = state, message
                account_error = state == "invalid" or job.remote_status.lower() == "error"
                if account_error and job.mode == "credentials":
                    await self._retry_relogin(job)
                elif state == "monitoring":
                    # A confirmed healthy poll starts a fresh retry budget for the next incident.
                    job.attempts, job.next_relogin = 0, None
                    if not job.model_restrictions.get("model_mapping"):
                        job.state, job.message = "attention", "未设置模型限制，请选择已配置的参考账号重新导入；持续监控中"
            except asyncio.CancelledError:
                raise
            except AdminAPIError as exc:
                if exc.status_code == 404:
                    job.state, job.message = "removed", "Sub2API 账号已删除，停止监控"
                    await self._save(job)
                    return
                job.state, job.message = "attention", "状态查询失败，继续监控并稍后重试"
            except Exception:
                job.state, job.message = "attention", "状态查询失败，继续监控并稍后重试"
            try:
                await self._save(job)
            except StorageError:
                job.message = "Redis 保存失败，将继续重试；原始资料仍保留"
            await asyncio.sleep(self.poll_seconds)

    async def _retry_relogin(self, job: Job) -> None:
        if not job.credential_line:
            job.message = "缺少首次导入资料，无法自动授权；持续监控中"
            return
        if job.attempts >= MAX_RELOGIN_ATTEMPTS:
            job.state, job.message = "invalid", "首次授权及 3 次重试均未恢复，等待手动处理；持续监控中"
            return
        if job.next_relogin and _now() < job.next_relogin:
            job.message = "等待下一次自动授权；持续监控中"
            return
        job.attempts += 1
        job.next_relogin = _now() + timedelta(seconds=self.retry_seconds)
        job.state, job.message = "reauthorizing", f"正在自动授权（第 {job.attempts}/{MAX_RELOGIN_ATTEMPTS} 次）"
        try:
            await self._save(job)
            async with self._import_lock:
                choice = select_5x_proxy(await self.client.list_active_proxies())
                async with self._login_lock:
                    payload = await asyncio.to_thread(
                        self.login, job.credential_line, "sub2api",
                        group_ids=job.group_ids, proxy=choice.url,
                    )
                accounts = validate_bundle(payload)["accounts"]
                if len(accounts) != 1 or accounts[0]["name"].casefold() != job.name.casefold():
                    raise ValueError("重新授权账号不匹配")
                await self.client.apply_oauth(job.account_id, accounts[0])
                await self.client.update_settings(job.account_id, _proxy_settings(choice.id))
                job.proxy_id, job.login_proxy = choice.id, choice.url
            job.state, job.message = "monitoring", "重新授权已提交，等待状态恢复；持续监控中"
        except asyncio.CancelledError:
            raise
        except Exception:
            job.state = "invalid"
            job.message = ("首次授权及 3 次重试均失败，等待手动处理；持续监控中"
                           if job.attempts >= MAX_RELOGIN_ATTEMPTS else "自动授权失败，稍后重试；持续监控中")
        finally:
            job.next_relogin = _now() + timedelta(seconds=self.retry_seconds)
            await self._save(job)

    async def _begin_manual(self, job: Job, mode: str) -> None:
        if job.mode != mode or (mode == "file" and job.account_id is None) or job.retired or job.state == "removed":
            raise ValueError("此任务不能执行该重新授权操作，请使用最新导入任务")
        if job.manual_pending or job.state in {"importing", "reauthorizing"}:
            raise ValueError("此账号正在授权，请等待当前操作完成")
        job.manual_pending = True
        line, proxy = job.credential_line, job.login_proxy
        try:
            if job.task and not job.task.done():
                job.task.cancel()
                try:
                    await job.task
                except asyncio.CancelledError:
                    pass
            # Keep the original import credentials for one-click authorization.
            job.credential_line, job.login_proxy = line, proxy
            job.state, job.message = "reauthorizing", "正在手动重新授权"
            await self._save(job)
        except BaseException:
            job.manual_pending = False
            job.state, job.message = "attention", "授权未启动，请稍后重试"
            raise

    async def reauthorize_credentials(self, job_id: str) -> dict:
        job = self.get_job(job_id)
        line = job.credential_line
        if not line:
            raise ValueError("此任务没有首次导入的账号资料，请使用原始账号资料导入任务")
        email, _, _ = parse_account_line(line)
        if email.casefold() != job.name.casefold():
            raise ValueError("重新授权资料必须属于当前账号")
        choice = select_5x_proxy(await self.client.list_active_proxies()) if job.account_id is None else None
        await self._begin_manual(job, "credentials")
        job.credential_line = line
        if choice is not None:
            job.proxy_id, job.login_proxy = choice.id, choice.url
        job.task = asyncio.create_task(
            self._manual_credentials(job) if job.account_id else self._login_and_import(job, job.profile_account_id, choice)
        )
        return job.public()

    async def _manual_credentials(self, job: Job) -> None:
        try:
            async with self._import_lock:
                choice = select_5x_proxy(await self.client.list_active_proxies())
                job.login_proxy = choice.url
                async with self._login_lock:
                    payload = await asyncio.to_thread(
                        self.login, job.credential_line, "sub2api",
                        group_ids=job.group_ids, proxy=job.login_proxy,
                    )
                accounts = validate_bundle(payload)["accounts"]
                if len(accounts) != 1 or accounts[0]["name"].casefold() != job.name.casefold():
                    raise ValueError("重新授权账号不匹配")
                await self.client.apply_oauth(job.account_id, accounts[0])
                await self.client.update_settings(job.account_id, _proxy_settings(choice.id))
                job.proxy_id = choice.id
                job.attempts, job.next_relogin = 0, None
                await self._start_watch(job)
        except asyncio.CancelledError:
            raise
        except Exception:
            job.state, job.message = "invalid", "手动重新授权失败，请核查账号资料和网络后重试"
        finally:
            job.manual_pending = False
            await self._save(job)
            self._resume_watch(job)

    async def reauthorize_rt(self, job_id: str, refresh_token: str) -> dict:
        token = refresh_token.strip()
        if not token or len(token) > 16384:
            raise ValueError("请填写有效的 RT")
        job = self.get_job(job_id)
        await self._begin_manual(job, "file")
        job.task = asyncio.create_task(self._manual_rt(job, token))
        return job.public()

    async def _manual_rt(self, job: Job, token: str) -> None:
        try:
            async with self._import_lock:
                await self.client.refresh_oauth(job.account_id, token)
                await self._start_watch(job)
        except asyncio.CancelledError:
            raise
        except ValueError as exc:
            job.state, job.message = "invalid", str(exc)
        except Exception:
            job.state, job.message = "invalid", "RT 重新授权失败，请核查 RT 和网络；也可重新上传 JSON"
        finally:
            job.manual_pending = False
            await self._save(job)
            self._resume_watch(job)

    async def reauthorize_file(self, job_id: str, payload: dict) -> dict:
        job = self.get_job(job_id)
        accounts = validate_bundle(payload)["accounts"]
        if len(accounts) != 1 or accounts[0]["name"].casefold() != job.name.casefold():
            raise ValueError("重新授权文件必须只包含同名账号")
        await self._begin_manual(job, "file")
        try:
            async with self._import_lock:
                choice = select_5x_proxy(await self.client.list_active_proxies())
                await self.client.apply_oauth(job.account_id, accounts[0])
                await self.client.update_settings(job.account_id, _proxy_settings(choice.id))
                job.proxy_id = choice.id
                await self._start_watch(job)
        except Exception:
            job.state, job.message = "invalid", "重新授权失败，请核查文件和服务器"
        finally:
            job.manual_pending = False
        await self._save(job)
        self._resume_watch(job)
        return job.public()

    async def close(self) -> None:
        self._closing = True
        tasks = [job.task for job in self.jobs.values() if job.task and not job.task.done()]
        for task in tasks:
            task.cancel()
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        try:
            for job in self.jobs.values():
                await self._save(job)
        finally:
            for job in self.jobs.values():
                job.credential_line = job.login_proxy = None
            await self.store.close()
            await self.client.close()
