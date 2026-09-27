"""Narrow asynchronous client for the Sub2API administrator account API."""

from __future__ import annotations

import os
from datetime import datetime, timezone
from urllib.parse import urlparse

import httpx


class AdminAPIError(RuntimeError):
    pass


PLUGIN_CAPABILITIES = {
    "openai.oauth.outbound_transport.v1",
    "openai.oauth.codex_ticket_hook.v1",
}


def plugin_account_ids(plugin: dict) -> set[int]:
    bindings = [
        binding for binding in plugin.get("bindings", [])
        if isinstance(binding, dict) and binding.get("enabled") is True
        and binding.get("platform") == "openai"
        and binding.get("account_type") == "oauth"
        and binding.get("capability") in PLUGIN_CAPABILITIES
    ]
    if plugin.get("state") != "enabled" or plugin.get("runtime_healthy") is not True or not bindings:
        raise ValueError("插件未启用、运行异常或不支持 OpenAI OAuth 账号，请刷新插件列表")
    ids = set()
    for binding in bindings:
        values = binding.get("account_ids")
        if not isinstance(values, list) or any(type(value) is not int or value <= 0 for value in values):
            raise AdminAPIError("插件账号范围响应结构无效")
        ids.update(values)
    return ids


class Sub2APIClient:
    def __init__(
        self,
        base_url: str,
        api_key: str,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        parsed = urlparse(base_url)
        if parsed.scheme != "https" and not (
            parsed.scheme == "http" and parsed.hostname in {"localhost", "127.0.0.1", "sub2api"}
        ):
            raise ValueError("Sub2API 地址必须使用 HTTPS（本机除外）")
        if not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
            raise ValueError("Sub2API 地址无效")
        if not api_key.strip():
            raise ValueError("缺少 Sub2API 管理员密钥")
        self.base_url = base_url.rstrip("/")
        self._http = httpx.AsyncClient(
            headers={"x-api-key": api_key.strip(), "accept": "application/json"},
            timeout=20.0,
            follow_redirects=False,
            trust_env=False,
            transport=transport,
        )

    @classmethod
    def from_env(cls) -> "Sub2APIClient":
        return cls(
            os.getenv("SUB2API_BASE_URL", ""),
            os.getenv("SUB2API_ADMIN_API_KEY", ""),
        )

    async def close(self) -> None:
        await self._http.aclose()

    async def _request(
        self, method: str, path: str, *, params: dict | None = None, payload: dict | None = None, timeout: float = 20.0
    ):
        url = f"{self.base_url}/api/v1/admin/{path.lstrip('/')}"
        try:
            response = await self._http.request(method, url, params=params, json=payload, timeout=timeout)
        except httpx.HTTPError as exc:
            raise AdminAPIError(f"管理员接口连接失败：{type(exc).__name__}") from None
        try:
            body = response.json()
        except ValueError:
            raise AdminAPIError(f"管理员接口返回非 JSON：HTTP {response.status_code}") from None
        if not isinstance(body, dict):
            raise AdminAPIError("管理员接口响应结构无效")
        if response.status_code >= 400 or body.get("code") not in (None, 0):
            code = body.get("code")
            raise AdminAPIError(f"管理员接口失败：HTTP {response.status_code}，代码 {code}")
        return body.get("data")

    async def find_account(self, name: str) -> dict | None:
        data = await self._request(
            "GET", "accounts", params={"search": name, "page": 1, "page_size": 100}
        )
        if not isinstance(data, dict) or not isinstance(data.get("items"), list):
            raise AdminAPIError("账号列表响应结构无效")
        matches = [
            item for item in data["items"]
            if isinstance(item, dict) and str(item.get("name", "")).casefold() == name.casefold()
        ]
        if len(matches) > 1:
            raise AdminAPIError("存在同名账号，无法安全确定目标")
        return matches[0] if matches else None

    async def list_openai_oauth_accounts(self) -> list[dict]:
        accounts = []
        page = 1
        while True:
            data = await self._request(
                "GET", "accounts",
                params={
                    "page": page, "page_size": 100, "platform": "openai",
                    "type": "oauth", "sort_by": "name", "sort_order": "asc",
                },
            )
            if not isinstance(data, dict) or not isinstance(data.get("items"), list):
                raise AdminAPIError("OpenAI OAuth 账号列表响应结构无效")
            items = [item for item in data["items"] if isinstance(item, dict)]
            accounts.extend(
                {"id": int(item["id"]), "name": str(item.get("name") or ""), "status": str(item.get("status") or "")}
                for item in items
                if item.get("platform") == "openai" and item.get("type") == "oauth"
            )
            total = int(data.get("total", len(accounts)))
            if not items or len(accounts) >= total:
                return accounts
            page += 1

    async def get_profile_account(self, account_id: int) -> dict:
        account = await self.get_account(account_id)
        if account.get("platform") != "openai" or account.get("type") != "oauth":
            raise ValueError("参照账号必须是 OpenAI OAuth 类型")
        return account

    async def import_data(self, payload: dict) -> dict:
        data = await self._request(
            "POST", "accounts/data",
            payload={"data": payload, "skip_default_group_bind": True},
        )
        if not isinstance(data, dict):
            raise AdminAPIError("账号导入响应结构无效")
        return data

    async def get_account(self, account_id: int) -> dict:
        data = await self._request("GET", f"accounts/{account_id}")
        if not isinstance(data, dict) or int(data.get("id", 0)) != account_id:
            raise AdminAPIError("账号详情响应结构无效")
        return data

    async def update_groups(self, account_id: int, group_ids: list[int]) -> None:
        await self._request("PUT", f"accounts/{account_id}", payload={"group_ids": group_ids})

    async def update_settings(self, account_id: int, settings: dict) -> None:
        await self._request("PUT", f"accounts/{account_id}", payload=settings)

    async def set_schedulable(self, account_id: int, schedulable: bool) -> None:
        await self._request("POST", f"accounts/{account_id}/schedulable", payload={"schedulable": schedulable})

    async def apply_oauth(self, account_id: int, account: dict) -> None:
        credentials = account.get("credentials")
        if not isinstance(credentials, dict) or not all(
            isinstance(credentials.get(key), str) and credentials[key]
            for key in ("access_token", "refresh_token")
        ):
            raise ValueError("重新授权文件缺少访问令牌或刷新令牌")
        await self._request(
            "POST", f"accounts/{account_id}/apply-oauth-credentials",
            payload={
                "type": "oauth",
                "credentials": credentials,
                "extra": account.get("extra") or {},
            },
        )

    async def refresh_oauth(self, account_id: int, refresh_token: str) -> None:
        remote = await self.get_profile_account(account_id)
        current = remote.get("credentials") or {}
        extra = remote.get("extra") or {}
        payload = {"refresh_token": refresh_token}
        if remote.get("proxy_id") is not None:
            payload["proxy_id"] = remote["proxy_id"]
        if current.get("client_id"):
            payload["client_id"] = current["client_id"]
        info = await self._request("POST", "openai/refresh-token", payload=payload, timeout=120.0)
        if not isinstance(info, dict) or not isinstance(info.get("access_token"), str) or not info["access_token"].strip():
            raise ValueError("RT 未返回有效的访问令牌")
        # Verify identity before applying credentials to an existing account.
        email = current.get("email") or extra.get("email")
        if not email and "@" in str(remote.get("name", "")):
            email = remote["name"]
        matched = False
        for key, expected in (("email", email), ("chatgpt_user_id", current.get("chatgpt_user_id")),
                              ("chatgpt_account_id", current.get("chatgpt_account_id"))):
            actual = info.get(key)
            if expected and actual:
                left, right = str(expected).strip(), str(actual).strip()
                if key == "email":
                    left, right = left.casefold(), right.casefold()
                if left != right:
                    raise ValueError("RT 所属账号与当前账号不一致，未写入授权")
                matched = True
        if not matched:
            raise ValueError("无法核实 RT 所属账号，未写入授权；请重新上传同账号 JSON")
        credentials = dict(current)
        for key in ("access_token", "id_token", "email", "chatgpt_account_id", "chatgpt_user_id",
                    "organization_id", "plan_type", "subscription_expires_at", "client_id"):
            if info.get(key):
                credentials[key] = info[key]
        # OAuth may retain the submitted refresh token when it is not rotated.
        credentials["refresh_token"] = info.get("refresh_token") or refresh_token
        expires_at = info.get("expires_at")
        if isinstance(expires_at, (int, float)) and not isinstance(expires_at, bool) and expires_at > 0:
            credentials["expires_at"] = datetime.fromtimestamp(expires_at, timezone.utc).isoformat()
        await self.apply_oauth(account_id, {"credentials": credentials, "extra": extra})

    async def list_groups(self) -> list[dict]:
        data = await self._request("GET", "groups", params={"page": 1, "page_size": 100})
        items = data.get("items", []) if isinstance(data, dict) else []
        return [
            {"id": item.get("id"), "name": item.get("name"), "platform": item.get("platform")}
            for item in items if isinstance(item, dict)
        ]

    async def list_active_proxies(self) -> list[dict]:
        data = await self._request("GET", "proxies/all")
        if not isinstance(data, list):
            raise AdminAPIError("代理列表响应结构无效")
        return data

    async def list_enabled_plugins(self) -> list[dict]:
        data = await self._request("GET", "plugins")
        if not isinstance(data, list):
            raise AdminAPIError("插件列表响应结构无效")
        plugins = []
        for item in data:
            if not isinstance(item, dict):
                continue
            try:
                account_ids = plugin_account_ids(item)
            except ValueError:
                continue
            plugins.append({
                "id": item["id"], "name": item.get("name") or item.get("plugin_key"),
                "version": item.get("version", ""), "account_count": len(account_ids),
            })
        return plugins

    async def get_enabled_plugin(self, plugin_id: int) -> dict:
        if type(plugin_id) is not int or plugin_id <= 0:
            raise ValueError("插件 ID 无效")
        data = await self._request("GET", f"plugins/{plugin_id}")
        if not isinstance(data, dict) or data.get("id") != plugin_id:
            raise AdminAPIError("插件详情响应结构无效")
        plugin_account_ids(data)
        return data

    async def append_plugin_accounts(self, plugin_id: int, account_ids: list[int]) -> None:
        if not account_ids or any(type(value) is not int or value <= 0 for value in account_ids):
            raise ValueError("插件账号 ID 无效")
        # The existing API replaces the scope: fetch it immediately before merging.
        plugin = await self.get_enabled_plugin(plugin_id)
        existing = plugin_account_ids(plugin)
        merged = existing | set(account_ids)
        if merged == existing:
            return
        result = await self._request(
            "POST", f"plugins/{plugin_id}/enable",
            payload={"account_ids": sorted(merged), "accept_untested": False},
        )
        if not isinstance(result, dict) or result.get("id") != plugin_id:
            raise AdminAPIError("插件账号更新响应结构无效")
        if not merged.issubset(plugin_account_ids(result)):
            raise AdminAPIError("插件账号范围未完整保存，请在 Sub2API 核查")
