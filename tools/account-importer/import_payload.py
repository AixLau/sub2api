"""Validate uploaded Sub2API account bundles before any administrator write."""

from __future__ import annotations

import json

MAX_UPLOAD_BYTES = 2 * 1024 * 1024
MAX_ACCOUNTS = 50


def parse_bundle(raw: bytes) -> dict:
    if not raw or len(raw) > MAX_UPLOAD_BYTES:
        raise ValueError("请选择不超过 2 MB 的 JSON 文件")
    try:
        document = json.loads(raw.decode("utf-8-sig"))
    except (UnicodeError, json.JSONDecodeError) as exc:
        raise ValueError("文件不是有效的 UTF-8 JSON") from exc
    if isinstance(document, dict) and isinstance(document.get("data"), dict):
        document = document["data"]
    return validate_bundle(document)


def validate_bundle(document: object) -> dict:
    if not isinstance(document, dict):
        raise ValueError("Sub2API 文件顶层必须是对象")
    proxies = document.get("proxies")
    accounts = document.get("accounts")
    if not isinstance(proxies, list) or len(proxies) > MAX_ACCOUNTS:
        raise ValueError("proxies 必须是至多 50 项的数组")
    if not isinstance(accounts, list) or not 1 <= len(accounts) <= MAX_ACCOUNTS:
        raise ValueError("accounts 必须包含 1 至 50 个账号")
    names: set[str] = set()
    for account in accounts:
        if not isinstance(account, dict):
            raise ValueError("账号条目必须是对象")
        name = account.get("name")
        if not isinstance(name, str) or not name.strip() or len(name) > 200:
            raise ValueError("账号名称无效")
        normalized_name = name.strip().casefold()
        if normalized_name in names:
            raise ValueError("文件中存在重复的账号名称")
        names.add(normalized_name)
        if account.get("platform") != "openai" or account.get("type") != "oauth":
            raise ValueError("目前支持导入 OpenAI OAuth 账号")
        credentials = account.get("credentials")
        if not isinstance(credentials, dict) or not all(
            isinstance(credentials.get(key), str) and credentials[key]
            for key in ("access_token", "refresh_token")
        ):
            raise ValueError("账号缺少访问令牌或刷新令牌")
        if account.get("extra") is not None and not isinstance(account["extra"], dict):
            raise ValueError("extra 必须是对象")
        groups = account.get("group_ids", [])
        if not isinstance(groups, list) or len(groups) > 20 or any(
            isinstance(group, bool) or not isinstance(group, int) or group <= 0
            for group in groups
        ):
            raise ValueError("group_ids 必须是正整数数组")
    return document
