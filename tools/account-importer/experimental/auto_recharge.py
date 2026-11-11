#!/usr/bin/env python3
"""ChatGPT Plus 自动订阅：登录 -> 创建 checkout -> 填卡 -> Subscribe 支付。"""

from __future__ import annotations

import argparse
import asyncio
import base64
import http.cookiejar
import json
import os
import re
import socket
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from urllib.parse import quote, urlencode, urlsplit

IMPORTER = Path(__file__).resolve().parents[1]
if str(IMPORTER) not in sys.path:
    sys.path.insert(0, str(IMPORTER))

from local_relogin import _load_upstream, parse_account_line

TEST_CARD = {
    "number": "4242 4242 4242 4242",
    "expiry": "12 / 34",
    "cvc": "123",
}
DEFAULT_MOCK_BILLING_URL = "https://geomock.com/api/address?country_code=US&area_code=DE"
DEFAULT_BILLING_API_URL = "https://www.usaddress.cn/api/generate.php"
MOCK_BILLING = {
    "name": "Test User",
    "line1": "123 Test Street",
    "line2": "",
    "city": "Dover",
    "state": "DE",
    "postal_code": "19901",
    "country": "US",
}


def _masked_email(email: str) -> str:
    local, separator, domain = email.partition("@")
    if not separator:
        return "[REDACTED]"
    return f"{local[:2]}***@{domain}"


def _account_line(args: argparse.Namespace) -> str:
    if args.account_file:
        return Path(args.account_file).expanduser().read_text(encoding="utf-8").strip()
    if args.account_line:
        return args.account_line.strip()
    raise SystemExit("需要 --account-file 或 --account-line")


class _SocksRelay:
    """本地免认证 SOCKS5 中继：代替客户端向上游 SOCKS5 做用户名/密码认证。

    Chromium 不支持带认证的 SOCKS5，curl_cffi 对部分代理的认证也不稳，
    统一切到本机 relay 可以同时绕开这两个限制。
    """

    def __init__(self, upstream: str):
        parts = urlsplit(upstream)
        if not parts.hostname or not parts.port:
            raise SystemExit("上游 socks5 代理格式无效")
        self.upstream_host = parts.hostname
        self.upstream_port = parts.port
        self.username = parts.username or ""
        self.password = parts.password or ""
        self._server: asyncio.AbstractServer | None = None
        self.port = 0

    async def start(self) -> str:
        self._server = await asyncio.start_server(self._handle, "127.0.0.1", 0)
        self.port = self._server.sockets[0].getsockname()[1]
        return f"socks5://127.0.0.1:{self.port}"

    async def stop(self) -> None:
        if self._server is not None:
            self._server.close()
            try:
                await self._server.wait_closed()
            except Exception:
                pass

    async def _handle(self, reader, writer) -> None:
        upstream_writer = None
        try:
            version, method_count = await reader.readexactly(2)
            await reader.readexactly(method_count)
            writer.write(b"\x05\x00")
            await writer.drain()

            _, command, _, address_type = await reader.readexactly(4)
            if address_type == 1:
                host = socket.inet_ntoa(await reader.readexactly(4))
            elif address_type == 3:
                length = (await reader.readexactly(1))[0]
                host = (await reader.readexactly(length)).decode("idna")
            elif address_type == 4:
                host = socket.inet_ntop(socket.AF_INET6, await reader.readexactly(16))
            else:
                raise RuntimeError("unsupported address type")
            port = int.from_bytes(await reader.readexactly(2), "big")
            if command != 1:
                writer.write(b"\x05\x07\x00\x01" + b"\x00" * 6)
                await writer.drain()
                return

            upstream_reader, upstream_writer = await self._connect_upstream(host, port, address_type)
            writer.write(b"\x05\x00\x00\x01" + b"\x00" * 6)
            await writer.drain()
            await asyncio.gather(
                self._pipe(reader, upstream_writer),
                self._pipe(upstream_reader, writer),
            )
        except Exception:
            pass
        finally:
            for stream in (writer, upstream_writer):
                if stream is None:
                    continue
                try:
                    stream.close()
                except Exception:
                    pass

    async def _connect_upstream(self, host: str, port: int, address_type: int = 3):
        reader, writer = await asyncio.open_connection(self.upstream_host, self.upstream_port)
        try:
            if self.username:
                writer.write(b"\x05\x01\x02")
                await writer.drain()
                if (await reader.readexactly(2))[1] != 2:
                    raise RuntimeError("upstream rejected username/password auth")
                user = self.username.encode()
                password = self.password.encode()
                writer.write(
                    b"\x01" + bytes([len(user)]) + user + bytes([len(password)]) + password
                )
                await writer.drain()
                if (await reader.readexactly(2))[1] != 0:
                    raise RuntimeError("upstream auth failed")
            else:
                writer.write(b"\x05\x01\x00")
                await writer.drain()
                if (await reader.readexactly(2))[1] != 0:
                    raise RuntimeError("upstream requires auth")

            # 按客户端原始地址类型转发（IP 就不要伪装成域名）
            if address_type == 1:
                encoded_address = b"\x01" + socket.inet_aton(host)
            elif address_type == 4:
                encoded_address = b"\x04" + socket.inet_pton(socket.AF_INET6, host)
            else:
                name = host.encode("idna")
                encoded_address = b"\x03" + bytes([len(name)]) + name
            writer.write(b"\x05\x01\x00" + encoded_address + port.to_bytes(2, "big"))
            await writer.drain()
            head = await reader.readexactly(4)
            address_type = head[3]
            if address_type == 1:
                await reader.readexactly(4)
            elif address_type == 3:
                await reader.readexactly((await reader.readexactly(1))[0])
            elif address_type == 4:
                await reader.readexactly(16)
            await reader.readexactly(2)
            if head[1] != 0:
                raise RuntimeError(f"upstream connect failed code={head[1]}")
            return reader, writer
        except Exception:
            writer.close()
            raise

    @staticmethod
    async def _pipe(source, target) -> None:
        try:
            while True:
                data = await source.read(65536)
                if not data:
                    break
                target.write(data)
                await target.drain()
        except Exception:
            pass
        finally:
            try:
                target.close()
            except Exception:
                pass


def _normalize_proxy(raw: str) -> str:
    """统一代理 URL：补 scheme、对用户名/密码做百分号编码（空格等）。"""
    if "://" not in raw:
        raw = "http://" + raw
    parts = urlsplit(raw)
    if not parts.hostname or not parts.port:
        raise SystemExit("代理格式无效，需要 scheme://user:pass@host:port")
    user = quote(parts.username or "", safe="")
    password = quote(parts.password or "", safe="")
    auth = f"{user}:{password}@" if user else ""
    return f"{parts.scheme}://{auth}{parts.hostname}:{parts.port}"


def _proxy_settings(raw: str | None) -> dict | None:
    if not raw:
        return None
    if "://" not in raw:
        raw = "http://" + raw
    parts = urlsplit(raw)
    if not parts.hostname or not parts.port:
        raise SystemExit("代理格式无效，需要 http://host:port 或带用户名密码")
    settings = {"server": f"{parts.scheme}://{parts.hostname}:{parts.port}"}
    if parts.username:
        settings["username"] = parts.username
        settings["password"] = parts.password or ""
    return settings


def _jwt_claims(token: str) -> dict:
    try:
        payload = token.split(".")[1]
        payload += "=" * (-len(payload) % 4)
        return json.loads(base64.urlsafe_b64decode(payload))
    except Exception:
        return {}


def _cookies_for_playwright(flow) -> list[dict]:
    cookies = []
    for cookie in getattr(flow.session.cookies, "jar", []):
        name = getattr(cookie, "name", "")
        value = getattr(cookie, "value", "")
        if not name or not value:
            continue
        cookies.append(
            {
                "name": name,
                "value": value,
                "domain": getattr(cookie, "domain", "") or ".chatgpt.com",
                "path": getattr(cookie, "path", "") or "/",
                "secure": bool(getattr(cookie, "secure", False)),
                "httpOnly": bool(cookie.has_nonstandard_attr("HttpOnly")),
                "sameSite": "Lax",
                "expires": getattr(cookie, "expires", None) or -1,
            }
        )
    return cookies


def _login(account_line: str, source_dir: Path, proxy: str | None):
    email, password, secret = parse_account_line(account_line)
    AuthFlow, Config, MailboxUnavailableProvider, _make_cpa, _make_sub2api = _load_upstream(source_dir)

    def build_flow():
        flow = AuthFlow(
            Config(proxy=proxy),
            env_overrides={
                "WEBUI_ALLOW_LOGIN": "1",
                "OAUTH_REFRESH_ONLY": "1",
                "OAUTH_CODEX_RT_EXCHANGE": "1",
                "OAUTH_CODEX_RT_BEFORE_CALLBACK": "1",
                "OAUTH_CODEX_AFTER_CALLBACK": "1",
                "OAUTH_CODEX_PROMPT": "",
                "AUTH_TRACE_DUMP": "0",
                "AUTH_TRACE_INCLUDE_COOKIE": "0",
                "AUTH_HTTP_TRACE": "0",
            },
            account_callback=lambda _email: {"password": password, "totp_secret": secret},
        )
        flow.result.totp_secret = secret
        return flow

    flow = None
    result = None
    last_error = None
    access_token = ""
    for attempt in range(4):
        try:
            print(f"LOGIN_ATTEMPT {attempt + 1}", flush=True)
            flow = build_flow()
            provider = MailboxUnavailableProvider(email, "未配置邮箱收件接口")
            try:
                result = flow.run_protocol_login(provider, email, password)
            except Exception as exc:
                last_error = exc
                if flow is not None and "协议登录完成，但未拿到" in str(exc):
                    result = flow.result
                else:
                    raise
            access_token = str((result.access_token if result else "") or "")
            if not access_token and flow is not None:
                try:
                    response = flow.session.get(
                        "https://chatgpt.com/api/auth/session",
                        headers={"Accept": "application/json"},
                        timeout=30,
                    )
                    print(f"SESSION_STATUS {response.status_code}", flush=True)
                    access_token = str((response.json() or {}).get("accessToken") or "")
                except Exception as exc:
                    print(f"SESSION_FETCH_ERROR {type(exc).__name__}", flush=True)
                    last_error = exc
            if access_token:
                break
            last_error = last_error or RuntimeError("登录成功，但没有拿到 access token")
            if attempt < 3:
                print("LOGIN_RETRY NoAccessToken", flush=True)
                time.sleep(2)
                continue
        except Exception as exc:
            last_error = exc
            if attempt < 3:
                print(f"LOGIN_RETRY {type(exc).__name__}", flush=True)
                time.sleep(2)
                continue
            raise
    if result is None or not access_token:
        raise last_error or RuntimeError("登录失败")
    print(
        f"LOGIN_OK email={_masked_email(email)} "
        f"cookies={len(cookies_for := _cookies_for_playwright(flow))}",
        flush=True,
    )
    return flow, access_token, email, cookies_for


async def _wait_for_card_frame(page, timeout_seconds: float = 45):
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        for frame in page.frames:
            try:
                if await frame.locator("input[name='number']").count():
                    return frame
                if await frame.locator("input[autocomplete='cc-number']").count():
                    return frame
            except Exception:
                continue
        await asyncio.sleep(1)
    return None


async def _ensure_pricing_dialog(page) -> None:
    if await page.locator('[role="dialog"]').count():
        return
    # 1) 页面上的直达按钮
    for name in ("Upgrade plan", "Upgrade", "升级计划"):
        try:
            await page.get_by_role("button", name=re.compile(name, re.I)).first.click(timeout=3000)
            await page.wait_for_timeout(3000)
            if await page.locator('[role="dialog"]').count():
                return
        except Exception:
            continue
    # 2) 头像菜单里的 Upgrade
    profile_sels = (
        '[data-testid="profile-button"]',
        'button[aria-label*="profile" i]',
        'button:has-text("Free")',
        'button:has-text("Plus")',
    )
    item_sels = (
        '[data-testid*="menu" i] [role="menuitem"]',
        '[data-testid*="menu" i] button',
        '[data-testid*="menu" i] a',
        '[data-testid*="menu" i] div[role="button"]',
        '[role="menuitem"]',
        '[role="menu"] [role="button"]',
        '[role="menu"] a',
        'a[href*="pricing"]',
    )
    upgrade_re = re.compile(r"upgrade|plus|升级", re.I)
    for profile_sel in profile_sels:
        try:
            await page.locator(profile_sel).first.click(timeout=3000)
        except Exception:
            continue
        # 菜单内容是异步加载的（Loading profile menu…），要等 Upgrade 项出现
        for _ in range(12):
            clicked = False
            for item_sel in item_sels:
                try:
                    item = page.locator(item_sel).filter(has_text=upgrade_re).first
                    await item.click(timeout=2000)
                    clicked = True
                    break
                except Exception:
                    continue
            if not clicked:
                try:
                    item = page.get_by_text(re.compile(r"^\s*Upgrade\s*$", re.I)).last
                    await item.click(timeout=2000)
                    clicked = True
                except Exception:
                    pass
            if clicked:
                await page.wait_for_timeout(3000)
                if await page.locator('[role="dialog"]').count():
                    return
                break
            await page.wait_for_timeout(1000)
        try:
            await page.keyboard.press("Escape")
            await page.wait_for_timeout(500)
        except Exception:
            pass
    # 3) 兜底：直接访问 /gpts/pricing 或等待后重试一次
    try:
        await _safe_goto(page, "https://chatgpt.com/#pricing", 60000)
        await page.wait_for_timeout(8000)
        if await page.locator('[role="dialog"]').count():
            return
        await page.get_by_role("button", name=upgrade_re).first.click(timeout=3000)
        await page.wait_for_timeout(3000)
    except Exception:
        pass


async def _pricing_debug(page) -> dict:
    return await page.evaluate(
        """() => {
            const visible = (el) => {
                const r = el.getBoundingClientRect();
                const s = getComputedStyle(el);
                return r.width > 0 && r.height > 0 && s.display !== 'none' && s.visibility !== 'hidden';
            };
            const norm = (s) => (s || '').replace(/\\s+/g, ' ').trim();
            const dialogs = [...document.querySelectorAll('[role="dialog"], dialog')].filter(visible)
                .map((d) => norm(d.innerText).slice(0, 400));
            const buttons = [...document.querySelectorAll('button')].filter(visible)
                .map((b) => norm(b.innerText || b.getAttribute('aria-label')))
                .filter(Boolean).slice(0, 40);
            return {url: location.href, title: document.title, dialogs, buttons};
        }"""
    )


async def _scroll_pricing(page) -> None:
    """定价弹窗内容较多，滚到底部触发懒加载，避免卡片/按钮未渲染。"""
    try:
        await page.evaluate(
            """() => {
                for (const dialog of document.querySelectorAll('[role="dialog"]')) {
                    dialog.scrollTop = dialog.scrollHeight;
                }
                window.scrollTo(0, document.body.scrollHeight);
            }"""
        )
        await page.wait_for_timeout(800)
    except Exception:
        pass


async def _expand_all_plans(page) -> None:
    for selector in ('[data-see-all-plans-toggle]',):
        try:
            locator = page.locator(selector).first
            if await locator.count():
                await locator.click(timeout=3000)
                print("PLANS_EXPANDED", flush=True)
                await page.wait_for_timeout(1500)
                return
        except Exception:
            continue
    try:
        await page.get_by_text(re.compile(r"see all plans", re.I)).first.click(timeout=3000)
        print("PLANS_EXPANDED", flush=True)
        await page.wait_for_timeout(1500)
    except Exception:
        pass


async def _mark_plus_button(page) -> dict:
    return await page.evaluate(
        """() => {
            const visible = (el) => {
                const r = el.getBoundingClientRect();
                const s = getComputedStyle(el);
                return r.width > 0 && r.height > 0 && s.display !== 'none' && s.visibility !== 'hidden';
            };
            const norm = (s) => (s || '').replace(/\\s+/g, ' ').trim();
            const isCta = (t) => /^(get started|buy now|choose|select|subscribe|continue)$/i.test(t);
            const isPlanCta = (t) => /get started|buy now|choose|select|subscribe/i.test(t) && !/requires upgrade/i.test(t);
            const dialogs = [...document.querySelectorAll('[role="dialog"]')].filter(visible);
            const dialog = dialogs[dialogs.length - 1];
            const result = {candidates: [], chosen: null, plan: null, found: false};
            if (!dialog) return result;

            const nodes = [...dialog.querySelectorAll('*')];
            const plusIndex = nodes.findIndex(
                (el) => visible(el) && el.children.length <= 2 && /^plus$/i.test(norm(el.innerText))
            );
            if (plusIndex >= 0) {
                for (let i = plusIndex + 1; i < nodes.length; i++) {
                    const node = nodes[i];
                    if (node.tagName !== 'BUTTON' || !visible(node) || node.disabled) continue;
                    const text = norm(node.innerText);
                    if (!isPlanCta(text)) continue;
                    node.setAttribute('data-auto-checkout', '1');
                    result.chosen = text;
                    result.plan = 'Plus';
                    result.found = true;
                    return result;
                }
            }

            for (const node of nodes) {
                if (node.tagName !== 'BUTTON' || !visible(node) || node.disabled) continue;
                const text = norm(node.innerText);
                if (text) result.candidates.push(text);
            }
            result.candidates = result.candidates.slice(0, 12);
            return result;
        }"""
    )


async def _checkout_via_api(page, access_token: str, account_id: str, promo: str) -> dict | None:
    """用页面同源 fetch + Bearer token 直接创建 checkout，绕过定价弹窗 UI。"""
    payloads: list[dict] = []
    if promo:
        payloads.append({"promo_campaign": promo})
    payloads += [{}, {"plan": "plus"}, {"plan": "plus", "interval": "month"}]
    results = await page.evaluate(
        """async ({token, accountId, payloads}) => {
            const out = [];
            for (const payload of payloads) {
                const headers = {
                    'content-type': 'application/json',
                    authorization: 'Bearer ' + token,
                };
                if (accountId) headers['chatgpt-account-id'] = accountId;
                try {
                    const r = await fetch('/backend-api/payments/checkout', {
                        method: 'POST',
                        headers,
                        body: JSON.stringify(payload),
                    });
                    const text = await r.text();
                    let data = null;
                    try { data = JSON.parse(text); } catch (e) {}
                    out.push({payload, status: r.status, data, raw: text.slice(0, 300)});
                } catch (e) {
                    out.push({payload, status: 0, error: String(e)});
                }
            }
            return out;
        }""",
        {"token": access_token, "accountId": account_id, "payloads": payloads},
    )
    for item in results:
        status = item.get("status")
        data = item.get("data") or {}
        detail = data.get("detail") if isinstance(data, dict) else None
        print(
            f"CHECKOUT_API {status} "
            + json.dumps(detail or data or item.get("raw") or item.get("error"), ensure_ascii=False)[:300],
            flush=True,
        )
        detail_text = json.dumps(detail or data or "", ensure_ascii=False)
        if status == 400 and re.search(r"already paid|already subscribed", detail_text, re.I):
            return {"already_paid": True, "detail": detail_text[:200], "url": ""}
        url = data.get("url") or data.get("stripe_hosted_url") or data.get("hosted_checkout_url")
        if status == 200 and (url or data.get("checkout_session_id")):
            if not url:
                entity = data.get("processor_entity") or "openai_llc"
                url = (
                    f"https://chatgpt.com/checkout/{entity}/"
                    f"{data['checkout_session_id']}?source=chatgpt_web_merged"
                )
            return {"url": url, "data": data}
    return None


async def _safe_goto(page, url: str, timeout: int = 120000) -> None:
    """goto 可能被上一次导航打断，重试一次并等页面稳定。"""
    last_error: Exception | None = None
    for _ in range(2):
        try:
            await page.goto(url, wait_until="commit", timeout=timeout)
            try:
                await page.wait_for_load_state("domcontentloaded", timeout=15000)
            except Exception:
                pass
            return
        except Exception as exc:
            last_error = exc
            await page.wait_for_timeout(2000)
    if last_error is not None:
        raise last_error


async def _create_checkout(page, promo: str, access_token: str = "", account_id: str = "") -> dict:
    api = None
    if access_token:
        try:
            # 相对路径 fetch 需要页面已在 chatgpt.com 源上
            if not page.url.startswith("https://chatgpt.com"):
                await _safe_goto(page, "https://chatgpt.com/", 120000)
                await page.wait_for_timeout(3000)
            api = await _checkout_via_api(page, access_token, account_id, promo)
        except Exception as exc:
            print(f"CHECKOUT_API_ERROR {type(exc).__name__}", flush=True)
    if api:
        if api.get("already_paid"):
            print(f"ALREADY_SUBSCRIBED {api.get('detail', '')}", flush=True)
            return {"already_paid": True}
        print(f"CHECKOUT_VIA_API {api['url'].split('?')[0]}", flush=True)
        await _safe_goto(page, api["url"], 120000)
        await page.wait_for_timeout(8000)
        return api["data"]
    print("CHECKOUT_API_UNAVAILABLE; falling back to pricing UI", flush=True)

    pricing_url = "https://chatgpt.com/#pricing"
    if promo:
        pricing_url = f"https://chatgpt.com/?promo_campaign={quote(promo)}#pricing"
    print(f"OPEN_PRICING {pricing_url}", flush=True)
    marker = None
    for attempt in range(3):
        await _safe_goto(page, pricing_url, 120000)
        await page.wait_for_timeout(8000)
        await _ensure_pricing_dialog(page)
        await _scroll_pricing(page)
        await _expand_all_plans(page)
        await _scroll_pricing(page)
        marker = await _mark_plus_button(page)
        print("PLUS_BUTTONS", json.dumps(marker, ensure_ascii=False)[:3000], flush=True)
        if marker.get("found"):
            break
        print(f"PLUS_DIALOG_RETRY {attempt + 1}/3", flush=True)
        try:
            await page.wait_for_timeout(3000)
        except Exception as exc:
            raise RuntimeError(f"页面已关闭（{type(exc).__name__}），停止重试") from exc
    if not marker or not marker.get("found"):
        debug = await _pricing_debug(page)
        print("PRICING_DEBUG " + json.dumps(debug, ensure_ascii=False)[:3000], flush=True)
        raise RuntimeError(f"没有找到 Plus 套餐入口（可见 CTA: {[c.get('text') for c in (marker or {}).get('candidates', [])]}）")
    selected = page.locator('[data-auto-checkout="1"]')
    if not await selected.count():
        debug = await _pricing_debug(page)
        print("PRICING_DEBUG " + json.dumps(debug, ensure_ascii=False)[:3000], flush=True)
        raise RuntimeError("没有找到 Plus 的 Get started 按钮")

    print("CLICKING_PLUS_GET_STARTED", flush=True)
    async with page.expect_response(
        lambda response: response.request.method == "POST"
        and "/backend-api/payments/checkout" in response.url,
        timeout=120000,
    ) as info:
        await selected.first.click()
    response = await info.value
    text = await response.text()
    try:
        data = json.loads(text)
    except Exception:
        data = {"raw": text[:500]}
    print(f"CHECKOUT_STATUS {response.status}", flush=True)
    if response.status != 200:
        print(
            "CHECKOUT_ERROR "
            + json.dumps(
                {k: data.get(k) for k in ("detail", "message", "error") if data.get(k)},
                ensure_ascii=False,
            ),
            flush=True,
        )
        raise RuntimeError(f"checkout 创建失败：HTTP {response.status}")

    checkout_url = data.get("url") or data.get("stripe_hosted_url") or data.get("hosted_checkout_url")
    if not checkout_url:
        entity = data.get("processor_entity") or "openai_llc"
        checkout_id = data.get("checkout_session_id")
        if not checkout_id:
            raise RuntimeError("checkout 响应没有 session id")
        checkout_url = f"https://chatgpt.com/checkout/{entity}/{checkout_id}?source=chatgpt_web_merged"
    print(f"OPEN_CHECKOUT {checkout_url}", flush=True)
    await _safe_goto(page, checkout_url, 120000)
    await page.wait_for_timeout(8000)
    return data


async def _fill_first(page, selectors: list[str], value: str, *, select: bool = False) -> bool:
    if not value:
        return False
    for selector in selectors:
        try:
            locator = page.locator(selector)
            count = await locator.count()
        except Exception:
            continue
        for index in range(min(count, 5)):
            item = locator.nth(index)
            try:
                if not await item.is_visible():
                    continue
                if select:
                    try:
                        await item.select_option(label=value)
                    except Exception:
                        await item.select_option(value=value)
                else:
                    await item.fill(value)
                return True
            except Exception:
                continue
    return False


async def _reveal_billing(page) -> None:
    """账单地址区可能折叠在 Edit/Add 入口后面，先尝试展开。"""
    for name in ("Billing address", "Billing details", "Edit billing", "Add address", "Edit address"):
        for frame in [page.main_frame] + [f for f in page.frames if f is not page.main_frame]:
            for role in ("button", "link"):
                try:
                    loc = frame.get_by_role(role, name=re.compile(name, re.I)).first
                    if not await loc.count():
                        continue
                    await loc.click(timeout=2500)
                    print(f"BILLING_SECTION_OPENED {name}", flush=True)
                    await page.wait_for_timeout(1500)
                    return
                except Exception:
                    continue


async def _fill_billing(page, billing: dict) -> None:
    specs = [
        (
            "name",
            [
                "input[autocomplete*='name' i]",
                "input[name='name']",
                "input[aria-label*='Full name' i]",
                "input[placeholder*='Name' i]",
            ],
            False,
        ),
        (
            "country",
            [
                "select[autocomplete*='country' i]",
                "select[name='country']",
                "select[name='billingCountry']",
                "select[aria-label*='Country' i]",
            ],
            True,
        ),
        (
            "line1",
            [
                "input[autocomplete*='address-line1' i]",
                "input[name='address1']",
                "input[name='line1']",
                "input[aria-label*='Address line 1' i]",
                "input[placeholder*='Address line 1' i]",
                "input[placeholder*='Address' i]",
            ],
            False,
        ),
        (
            "city",
            [
                "input[autocomplete*='address-level2' i]",
                "input[name='city']",
                "input[aria-label*='City' i]",
                "input[placeholder*='City' i]",
            ],
            False,
        ),
        (
            "state",
            [
                "select[autocomplete*='address-level1' i]",
                "select[name='state']",
                "select[name='billingState']",
                "select[aria-label*='State' i]",
                "input[name='state']",
            ],
            True,
        ),
        (
            "postal_code",
            [
                "input[autocomplete*='postal-code' i]",
                "input[name='postalCode']",
                "input[name='zip']",
                "input[aria-label*='ZIP' i]",
                "input[placeholder*='ZIP' i]",
            ],
            False,
        ),
    ]
    filled = []
    frame_used = ""
    # Stripe 地址组件可能在 iframe 里，且加载晚于页面：最多重试 5 轮补齐缺项
    for attempt in range(5):
        frames = [page.main_frame] + [f for f in page.frames if f is not page.main_frame]
        for field, selectors, is_select in specs:
            if field in filled:
                continue
            for frame in frames:
                try:
                    ok = await _fill_first(frame, selectors, str(billing.get(field) or ""), select=is_select)
                except Exception:
                    continue
                if ok:
                    filled.append(field)
                    frame_used = (frame.url or "").split("?")[0]
                    if field == "country":
                        # 选完国家 Stripe 会重渲染地址表单，等它稳定
                        await page.wait_for_timeout(1800)
                    break
        if len(filled) == len(specs) or attempt == 4:
            break
        await page.wait_for_timeout(2000)
    if filled:
        print("BILLING_FIELDS_FILLED " + ",".join(filled) + f" frame={frame_used}", flush=True)
    else:
        print("BILLING_FIELDS_NOT_FOUND (no editable address inputs in page or iframes)", flush=True)
        inputs = []
        for frame in [page.main_frame] + [f for f in page.frames if f is not page.main_frame]:
            try:
                inputs.append(
                    await frame.evaluate(
                        """() => [...document.querySelectorAll('input, select')].map((el) =>
                            (el.name || el.id || '?')
                            + '[' + (el.type || el.tagName.toLowerCase()) + ']'
                            + ' autocomplete=' + (el.getAttribute('autocomplete') || '-')
                            + ' placeholder=' + (el.getAttribute('placeholder') || '-')
                        ).slice(0, 40)"""
                    )
                )
            except Exception:
                continue
        print("BILLING_INPUTS " + json.dumps(inputs, ensure_ascii=False)[:2000], flush=True)


async def _verify_billing(page) -> dict:
    """回读 Stripe 地址组件的实际值，确认没有被清空。"""
    values: dict = {}
    for frame in page.frames:
        try:
            chunk = await frame.evaluate(
                """() => {
                    const out = {};
                    for (const el of document.querySelectorAll('input, select')) {
                        const key = (el.getAttribute('autocomplete') || el.name || el.getAttribute('aria-label') || '').trim();
                        const value = (el.value || '').trim();
                        if (key && value) out[key] = value;
                    }
                    return out;
                }"""
            )
        except Exception:
            continue
        if isinstance(chunk, dict):
            values.update(chunk)
    return values


async def _fill_card(page, card: dict) -> bool:
    frame = await _wait_for_card_frame(page)
    if frame is None:
        print("CARD_FRAME_NOT_FOUND", flush=True)
        return False
    filled = []
    for selector, value in (
        ("input[name='number']", str(card.get("number") or "")),
        ("input[name='expiry']", str(card.get("expiry") or "")),
        ("input[name='cvc']", str(card.get("cvc") or "")),
    ):
        try:
            locator = frame.locator(selector)
            if await locator.count() and value:
                await locator.first.fill(value)
                filled.append(selector)
        except Exception:
            continue
    print(f"CARD_FIELDS_FILLED {len(filled)}/3", flush=True)
    return len(filled) == 3


SUBSCRIBE_TEXT_RE = (
    r"subscribe|^pay(\s|\$)|pay now|start .{0,24}(subscription|plan)"
    r"|confirm (and|&) pay|purchase|upgrade now"
)


async def _mark_subscribe_button(page) -> dict:
    return await page.evaluate(
        """(pattern) => {
            const visible = (el) => {
                const r = el.getBoundingClientRect();
                const s = getComputedStyle(el);
                return r.width > 0 && r.height > 0 && s.display !== 'none' && s.visibility !== 'hidden';
            };
            const norm = (s) => (s || '').replace(/\\s+/g, ' ').trim();
            const re = new RegExp(pattern, 'i');
            const nodes = [...document.querySelectorAll('button, [role="button"], input[type="submit"]')];
            const buttons = nodes.filter((b) => {
                if (!visible(b) || b.disabled || b.getAttribute('aria-disabled') === 'true') return false;
                const text = norm(b.innerText)
                    + ' ' + norm(b.getAttribute('aria-label'))
                    + ' ' + norm(b.value || '');
                return re.test(text);
            });
            const labels = buttons.map((b) =>
                norm(b.innerText || b.getAttribute('aria-label') || b.value || '')
            ).slice(0, 5);
            if (buttons.length) {
                buttons[0].setAttribute('data-auto-subscribe', '1');
            }
            return {found: buttons.length > 0, labels};
        }""",
        SUBSCRIBE_TEXT_RE,
    )


async def _subscribe(page, timeout_seconds: float = 90) -> dict:
    """在付款页（含 iframe）里找到并点击 Subscribe，等待支付确认响应。"""
    deadline = time.monotonic() + timeout_seconds
    print("WAITING_SUBSCRIBE_BUTTON", flush=True)
    while time.monotonic() < deadline:
        for frame in page.frames:
            try:
                marker = await _mark_subscribe_button(frame)
            except Exception:
                continue
            if not marker.get("found"):
                continue
            print(
                "SUBSCRIBE_BUTTON " + json.dumps(marker, ensure_ascii=False)[:300],
                flush=True,
            )
            button = frame.locator('[data-auto-subscribe="1"]').first
            try:
                async with page.expect_response(
                    lambda response: response.request.method == "POST"
                    and "r.stripe.com" not in response.url
                    and ("/backend-api/" in response.url or "stripe.com" in response.url),
                    timeout=90000,
                ) as info:
                    await button.click(timeout=10000)
                response = await info.value
                print(
                    f"SUBSCRIBE_RESPONSE {response.status} {response.url.split('?')[0]}",
                    flush=True,
                )
            except Exception as exc:
                print(f"SUBSCRIBE_WAIT_ERROR {type(exc).__name__}: {exc}", flush=True)
            return {"clicked": True, "labels": marker.get("labels", [])}
        await asyncio.sleep(1)
    print("SUBSCRIBE_BUTTON_NOT_FOUND", flush=True)
    return {"clicked": False}


async def _wait_subscription_result(page, timeout_seconds: float = 120) -> str:
    deadline = time.monotonic() + timeout_seconds
    success_re = re.compile(
        r"subscription (is )?(now )?active|you'?re (all set|subscribed)"
        r"|thank you|welcome to plus",
        re.I,
    )
    while time.monotonic() < deadline:
        if "/checkout" not in urlsplit(page.url).path:
            print(f"SUBSCRIBE_URL {page.url}", flush=True)
            return "left_checkout"
        try:
            body = await page.evaluate("() => (document.body.innerText || '').slice(0, 4000)")
            if success_re.search(body or ""):
                return "success_text"
        except Exception:
            pass
        await asyncio.sleep(2)
    # 超时：打印页面上的报错/提示，便于判断卡被拒还是别的原因
    try:
        alerts = await page.evaluate(
            """() => [...document.querySelectorAll('[role="alert"], [role="status"], [data-testid*="error" i]')]
                .map((el) => (el.innerText || '').replace(/\\s+/g, ' ').trim())
                .filter(Boolean).slice(0, 5)"""
        )
        body = await page.evaluate("() => (document.body.innerText || '').replace(/\\s+/g, ' ').slice(0, 1200)")
        print("SUBSCRIBE_ALERTS " + json.dumps(alerts, ensure_ascii=False)[:1000], flush=True)
        print("SUBSCRIBE_PAGE_TEXT " + body, flush=True)
        frames = []
        for frame in page.frames:
            try:
                text = await frame.evaluate(
                    "() => (document.body.innerText || '').replace(/\\s+/g, ' ').trim().slice(0, 400)"
                )
            except Exception:
                continue
            if text and re.search(r"declin|incorrect|invalid|not valid|could not|try again|error", text, re.I):
                frames.append(text)
        if frames:
            print("SUBSCRIBE_FRAME_ERRORS " + json.dumps(frames[:3], ensure_ascii=False)[:1200], flush=True)
    except Exception:
        pass
    return "timeout"


def _fetch_billing_api(url: str, api_key: str, state: str, city: str) -> dict | None:
    """usaddress.cn 风格接口：Bearer key + state/city 查询参数。"""
    query = urlencode({"state": state, "city": city})
    separator = "&" if "?" in url else "?"
    headers = {
        "accept": "application/json",
        "authorization": f"Bearer {api_key}",
        "user-agent": (
            "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
            "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
        ),
    }
    try:
        request = urllib.request.Request(f"{url}{separator}{query}", headers=headers)
        with urllib.request.urlopen(request, timeout=20) as response:
            data = json.loads(response.read().decode("utf-8"))
        print(f"BILLING_API_STATUS 200 state={state} city={city}", flush=True)
        if isinstance(data, dict) and data.get("success") is False:
            print(
                "BILLING_API_ERROR "
                + json.dumps({k: data.get(k) for k in ("error", "code", "message")}, ensure_ascii=False)[:300],
                flush=True,
            )
            return None
        return data
    except urllib.error.HTTPError as exc:
        body = ""
        try:
            body = exc.read().decode("utf-8", "ignore")[:200]
        except Exception:
            pass
        print(f"BILLING_API_HTTP_{exc.code} {body}", flush=True)
        return None
    except Exception as exc:
        print(f"BILLING_API_FAILED {type(exc).__name__}", flush=True)
        return None


def _prefetch_billing_http(state: str, city: str) -> dict | None:
    """纯 HTTP 预取：GET /zh/ 取 frontendToken + PHPSESSID，再 POST generate.php。"""
    ua = (
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
        "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
    )
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    try:
        page_req = urllib.request.Request(
            "https://www.usaddress.cn/zh/",
            headers={"user-agent": ua, "accept": "text/html", "accept-language": "zh-CN,zh;q=0.9"},
        )
        html = opener.open(page_req, timeout=30).read().decode("utf-8", "ignore")
        match = re.search(r"frontendToken\s*:\s*'([^']+)'", html)
        if not match:
            print("BILLING_PREFETCH_NO_TOKEN", flush=True)
            return None
        token = match.group(1)
        body = {"tax_free": "0", "has_number": "1"}
        if state:
            body["state"] = state
        if city:
            body["city"] = city
        headers = {
            "content-type": "application/json",
            "accept": "*/*",
            "origin": "https://www.usaddress.cn",
            "referer": "https://www.usaddress.cn/zh/",
            "user-agent": ua,
            "sec-fetch-site": "same-origin",
            "x-frontend-token": token,
        }
        request = urllib.request.Request(
            "https://www.usaddress.cn/api/generate.php",
            data=json.dumps(body).encode(),
            headers=headers,
        )
        with opener.open(request, timeout=30) as response:
            data = json.loads(response.read().decode("utf-8"))
        print(
            f"BILLING_PREFETCH {response.status} state={state} city={city} "
            f"cookies={[c.name for c in jar]}",
            flush=True,
        )
        if isinstance(data, dict) and data.get("success") is False:
            print("BILLING_PREFETCH_ERROR " + json.dumps(data, ensure_ascii=False)[:300], flush=True)
            return None
        return data
    except urllib.error.HTTPError as exc:
        body_text = ""
        try:
            body_text = exc.read().decode("utf-8", "ignore")[:200]
        except Exception:
            pass
        print(f"BILLING_PREFETCH_HTTP_{exc.code} {body_text}", flush=True)
        return None
    except Exception as exc:
        print(f"BILLING_PREFETCH_FAILED {type(exc).__name__}: {exc}", flush=True)
        return None


async def _fetch_billing_usaddress(page, state: str, city: str) -> dict | None:
    """在 usaddress 站点上下文里用自带 token + 会话调用 generate.php（无需付费 key）。"""
    helper = None
    try:
        helper = await page.context.new_page()
        await helper.goto("https://www.usaddress.cn/zh/", wait_until="domcontentloaded", timeout=60000)
        await helper.wait_for_timeout(2000)
        result = await helper.evaluate(
            """async ({state, city}) => {
                const r = await fetch('/api/generate.php', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-Frontend-Token': window.appData.frontendToken,
                    },
                    credentials: 'include',
                    body: JSON.stringify({state, city}),
                });
                return {status: r.status, text: (await r.text()).slice(0, 4000)};
            }""",
            {"state": state, "city": city},
        )
        print(f"BILLING_API_STATUS {result['status']} state={state} city={city}", flush=True)
        if result["status"] != 200:
            print("BILLING_API_BODY " + result["text"][:300], flush=True)
            return None
        data = json.loads(result["text"])
        if isinstance(data, dict) and data.get("success") is False:
            print("BILLING_API_ERROR " + json.dumps(data, ensure_ascii=False)[:300], flush=True)
            return None
        return data
    except Exception as exc:
        print(f"BILLING_API_FAILED {type(exc).__name__}: {exc}", flush=True)
        return None
    finally:
        if helper is not None:
            try:
                await helper.close()
            except Exception:
                pass


def _normalize_billing(data: dict) -> dict:
    """兼容 geomock 等接口的地址结构。"""
    if not isinstance(data, dict):
        raise ValueError("响应不是对象")
    if isinstance(data.get("data"), dict):
        data = data["data"]
    first = data.get("firstname") or data.get("first_name") or data.get("firstName") or ""
    last = data.get("lastname") or data.get("last_name") or data.get("lastName") or ""
    full_name = f"{first} {last}".strip()
    address = data.get("address")
    merged = dict(data)
    if isinstance(address, dict):
        merged.update(address)

    def pick(*keys):
        for key in keys:
            value = merged.get(key)
            if isinstance(value, str) and value.strip():
                return value.strip()
        return ""

    country = pick("country_code", "countryCode", "country")
    if country.lower() in ("united states", "usa", "us"):
        country = "US"

    normalized = {
        "name": pick("name", "full_name", "fullName", "recipient") or full_name or MOCK_BILLING["name"],
        "line1": pick("line1", "address", "address1", "street", "street_address"),
        "line2": pick("line2", "address2", "street2"),
        "city": pick("city", "town"),
        "state": pick("state_code", "stateFullName", "stateName", "state", "province", "area_code"),
        "postal_code": pick("postal_code", "postalCode", "zip", "zipCode", "zipcode", "postcode"),
        "country": country or "US",
    }
    print("MOCK_BILLING_KEYS " + json.dumps(sorted(merged.keys()))[:500], flush=True)
    return normalized


def _fetch_mock_billing_http(url: str) -> dict | None:
    headers = {
        "accept": "*/*",
        "accept-language": "zh-CN,zh;q=0.9",
        "referer": "https://geomock.com/zh",
        "user-agent": (
            "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
            "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
        ),
    }
    try:
        request = urllib.request.Request(url, headers=headers)
        with urllib.request.urlopen(request, timeout=15) as response:
            return json.loads(response.read().decode("utf-8"))
    except Exception as exc:
        print(f"MOCK_BILLING_HTTP_FAILED {type(exc).__name__}", flush=True)
        return None


async def _fetch_mock_billing_in_page(page, url: str) -> dict | None:
    """geomock 接口只对页面同源请求开放，用浏览器在 geomock 源内 fetch。"""
    parts = urlsplit(url)
    if not parts.scheme or not parts.netloc:
        return None
    origin = f"{parts.scheme}://{parts.netloc}"
    relative = parts.path or "/"
    if parts.query:
        relative += "?" + parts.query
    helper = None
    try:
        helper = await page.context.new_page()
        await helper.goto(origin + "/zh", wait_until="domcontentloaded", timeout=60000)
        result = await helper.evaluate(
            """async (u) => {
                const r = await fetch(u, {headers: {accept: '*/*'}, credentials: 'include'});
                return {status: r.status, text: (await r.text()).slice(0, 4000)};
            }""",
            relative,
        )
        print(f"MOCK_BILLING_FETCH {result['status']}", flush=True)
        if result["status"] != 200:
            return None
        return json.loads(result["text"])
    except Exception as exc:
        print(f"MOCK_BILLING_FETCH_FAILED {type(exc).__name__}", flush=True)
        return None
    finally:
        if helper is not None:
            try:
                await helper.close()
            except Exception:
                pass


def _load_card(path: str | None) -> dict:
    if not path:
        return dict(TEST_CARD)
    data = json.loads(Path(path).expanduser().read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise SystemExit("card JSON 必须是对象")
    expiry = str(data.get("expiry") or "").strip()
    if not expiry and data.get("exp_month") and data.get("exp_year"):
        expiry = f"{int(data['exp_month']):02d} / {str(data['exp_year'])[-2:]}"
    return {
        "number": str(data.get("number") or ""),
        "expiry": expiry,
        "cvc": str(data.get("cvc") or ""),
    }


def _parse_money(text: str, label: str) -> float | None:
    match = re.search(label + r"[^0-9$]{0,12}\$?\s*([0-9]+(?:\.[0-9]+)?)", text, re.I)
    return float(match.group(1)) if match else None


async def _read_totals(page, checkout_data: dict | None = None) -> dict:
    body = ""
    try:
        body = await page.evaluate(
            "() => (document.body.innerText || '').replace(/\\s+/g, ' ').trim()"
        )
    except Exception:
        pass
    totals = {
        "subscription": _parse_money(body, r"monthly subscription"),
        "tax": _parse_money(body, r"estimated tax"),
        "due_today": _parse_money(body, r"due today"),
    }
    if totals["tax"] is None:
        totals["tax"] = _parse_money(body, r"tax\s*\([^)]*\)")
    # 页面在税额为 0 时可能不渲染税费行：实付等于订阅价即可判定税为 0
    if (
        totals["tax"] is None
        and totals["subscription"] is not None
        and totals["due_today"] is not None
        and abs(totals["due_today"] - totals["subscription"]) < 0.01
    ):
        totals["tax"] = 0.0
        totals["tax_inferred"] = True
    # 接口侧金额（最小货币单位），页面没读到时兜底
    try:
        state = (checkout_data or {}).get("checkout_state") or {}
        total = state.get("total") if isinstance(state, dict) else None
        if isinstance(total, dict) and totals["tax"] is None:
            for key in ("tax", "taxAmount", "tax_amount"):
                if isinstance(total.get(key), (int, float)):
                    totals["tax"] = round(float(total[key]) / 100.0, 2)
                    break
    except Exception:
        pass
    return totals


async def _print_summary(page, checkout_data: dict, account_plan: str | None) -> None:
    body = ""
    try:
        body = await page.evaluate("() => (document.body.innerText || '').replace(/\\s+/g, ' ').trim()")
    except Exception:
        pass
    due = re.search(r"Due today\s*\$?([0-9]+(?:\.[0-9]+)?)", body, re.I)
    tax = re.search(r"Estimated tax\s*\$?([0-9]+(?:\.[0-9]+)?)", body, re.I)
    print("ACCOUNT_PLAN " + str(account_plan), flush=True)
    try:
        print("PAGE_URL " + page.url, flush=True)
        print("PAGE_TITLE " + await page.title(), flush=True)
    except Exception as exc:
        print(f"PAGE_CLOSED {type(exc).__name__}", flush=True)
    if due:
        print("DUE_TODAY " + due.group(1), flush=True)
    if tax:
        print("ESTIMATED_TAX " + tax.group(1), flush=True)
    if checkout_data:
        safe = {
            key: checkout_data.get(key)
            for key in (
                "checkout_session_id",
                "processor_entity",
                "checkout_ui_mode",
                "payment_status",
                "status",
                "requires_manual_approval",
            )
            if checkout_data.get(key) is not None
        }
        state = checkout_data.get("checkout_state") or {}
        if isinstance(state, dict):
            safe["canConfirm"] = state.get("canConfirm")
            total = state.get("total") or {}
            if isinstance(total, dict):
                safe["total"] = total.get("total")
        print("CHECKOUT_SUMMARY " + json.dumps(safe, ensure_ascii=False), flush=True)


async def _run(args: argparse.Namespace) -> None:
    source_dir = Path(args.source_dir).expanduser().resolve()
    account_line = _account_line(args)
    proxy = _normalize_proxy(args.proxy) if args.proxy else None
    if proxy and proxy.startswith(("socks5://", "socks5h://")) and urlsplit(proxy).username:
        upstream_host = urlsplit(proxy).hostname
        proxy = await _SocksRelay(proxy).start()
        print(f"PROXY_RELAY {proxy} -> {upstream_host}", flush=True)

    # 打开 ChatGPT 浏览器前先把账单地址取好（纯 HTTP，不需要浏览器）
    prefetched_billing = None
    if args.fill_billing or args.mock_billing_url:
        raw = None
        if args.billing_api == "usaddress":
            raw = _prefetch_billing_http(args.billing_state, args.billing_city)
        if raw is None and args.billing_api_key:
            raw = _fetch_billing_api(
                args.billing_api_url, args.billing_api_key, args.billing_state, args.billing_city
            )
        if raw is not None:
            try:
                prefetched_billing = _normalize_billing(raw)
                print(
                    "BILLING_PREFETCH_DATA "
                    + json.dumps(prefetched_billing, ensure_ascii=False)[:400],
                    flush=True,
                )
            except Exception as exc:
                print(f"BILLING_PREFETCH_PARSE_FAILED {type(exc).__name__}", flush=True)

    flow, access_token, email, cookies = _login(account_line, source_dir, proxy)
    try:
        egress = flow.session.get("https://api.ipify.org", timeout=20)
        print(f"LOGIN_EGRESS_IP {(egress.text or '').strip()[:40]}", flush=True)
    except Exception as exc:
        print(f"LOGIN_EGRESS_IP_ERROR {type(exc).__name__}", flush=True)
    claims = _jwt_claims(access_token)
    auth = claims.get("https://api.openai.com/auth") or {}
    account_plan = auth.get("chatgpt_plan_type")
    account_id = str(auth.get("chatgpt_account_id") or "")
    print(f"ACCOUNT {_masked_email(email)} plan={account_plan} id={account_id[:8]}...", flush=True)

    from playwright.async_api import async_playwright

    proxy_settings = _proxy_settings(proxy)
    async with async_playwright() as playwright:
        launch_kwargs = {
            "headless": bool(args.headless),
            "proxy": proxy_settings,
            "args": ["--disable-blink-features=AutomationControlled"],
        }
        if proxy_settings is None:
            # 不指定代理时强制直连，避免 Chrome 走系统代理（Clash）
            launch_kwargs["args"] += ["--no-proxy-server"]
        if not args.headless:
            launch_kwargs["args"] += ["--start-maximized"]
        try:
            browser = await playwright.chromium.launch(channel=args.browser_channel, **launch_kwargs)
        except Exception:
            browser = await playwright.chromium.launch(**launch_kwargs)
        browser.on("disconnected", lambda *_: print("BROWSER_DISCONNECTED", flush=True))

        context_options: dict = {
            "locale": "en-US",
            "ignore_https_errors": True,
            "user_agent": (
                "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
                "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
            ),
        }
        if args.headless:
            context_options["viewport"] = {"width": 1440, "height": 1200}
        else:
            # 有头模式让视口跟随最大化窗口，避免超出屏幕被裁切
            context_options["no_viewport"] = True
        context = await browser.new_context(**context_options)
        for cookie in cookies:
            try:
                await context.add_cookies([cookie])
            except Exception:
                continue

        page = await context.new_page()
        page.on("close", lambda *_: print("PAGE_CLOSE_EVENT", flush=True))
        page.on("crash", lambda *_: print("PAGE_CRASH_EVENT", flush=True))
        try:
            egress = await context.request.get("https://api.ipify.org", timeout=30000)
            print(f"BROWSER_EGRESS_IP {(await egress.text()).strip()[:40]}", flush=True)
        except Exception as exc:
            print(f"BROWSER_EGRESS_IP_ERROR {type(exc).__name__}", flush=True)
        try:
            if args.checkout_url:
                print(f"OPEN_EXISTING_CHECKOUT {args.checkout_url}", flush=True)
                await _safe_goto(page, args.checkout_url, 120000)
                await page.wait_for_timeout(8000)
                checkout_data = {}
            else:
                checkout_data = await _create_checkout(page, args.promo, access_token, account_id)

            if isinstance(checkout_data, dict) and checkout_data.get("already_paid"):
                print("SUBSCRIBE_SKIP: 该账号已是 Plus，无需订阅", flush=True)
                await _print_summary(page, {}, account_plan)
                if args.screenshot:
                    try:
                        await page.screenshot(path=args.screenshot, full_page=True)
                        print(f"SCREENSHOT {args.screenshot}", flush=True)
                    except Exception as exc:
                        print(f"SCREENSHOT_FAILED {type(exc).__name__}", flush=True)
                print(f"KEEP_OPEN {args.keep_open}s", flush=True)
                await page.wait_for_timeout(max(0, args.keep_open) * 1000)
                return

            card_frame = await _wait_for_card_frame(page)
            if card_frame is None:
                print("PAYMENT_FORM_NOT_FOUND", flush=True)
                print("FRAMES " + json.dumps([f.url.split("?")[0] for f in page.frames], ensure_ascii=False), flush=True)
                raise RuntimeError("没有找到 Stripe 卡号输入框")

            if not args.no_fill:
                await _fill_card(page, _load_card(args.card_file))

            # 卡号填完后 Stripe 才会渲染/展开账单地址区
            if args.fill_billing or args.mock_billing_url:
                await page.wait_for_timeout(2000)
                await _reveal_billing(page)
                raw = None
                billing = prefetched_billing
                if billing is None:
                    if args.billing_api == "usaddress":
                        raw = await _fetch_billing_usaddress(page, args.billing_state, args.billing_city)
                    if raw is None and args.billing_api_key:
                        raw = _fetch_billing_api(
                            args.billing_api_url, args.billing_api_key, args.billing_state, args.billing_city
                        )
                    if raw is None:
                        mock_url = args.mock_billing_url or DEFAULT_MOCK_BILLING_URL
                        raw = await _fetch_mock_billing_in_page(page, mock_url)
                        if raw is None:
                            raw = _fetch_mock_billing_http(mock_url)
                    if raw is not None:
                        try:
                            billing = _normalize_billing(raw)
                        except Exception as exc:
                            print(f"BILLING_PARSE_FAILED {type(exc).__name__}; using static test address", flush=True)
                            billing = dict(MOCK_BILLING)
                    else:
                        billing = dict(MOCK_BILLING)
                print("BILLING_DATA " + json.dumps(billing, ensure_ascii=False)[:400], flush=True)
                await _fill_billing(page, billing)
                verified = await _verify_billing(page)
                print("BILLING_VERIFY " + json.dumps(verified, ensure_ascii=False)[:600], flush=True)

            if args.subscribe:
                totals = await _read_totals(page, checkout_data)
                print(
                    "CHECKOUT_TOTALS "
                    + json.dumps({k: v for k, v in totals.items() if k != "body"}),
                    flush=True,
                )
                tax = totals.get("tax")
                zero_tax_ok = tax is not None and abs(tax) < 0.005
                if not zero_tax_ok and not args.allow_tax:
                    reason = "unknown" if tax is None else f"{tax}"
                    print(f"TAX_CHECK_FAIL tax={reason}; 未点击 Subscribe（加 --allow-tax 可跳过）", flush=True)
                else:
                    subscribe_result = await _subscribe(page)
                    if subscribe_result.get("clicked"):
                        state = await _wait_subscription_result(page)
                        print(f"SUBSCRIBE_STATE {state}", flush=True)
                    else:
                        print("TEST_FLOW_ONLY: 没找到 Subscribe 按钮，未发起支付", flush=True)
            else:
                print("TEST_FLOW_ONLY: 未加 --subscribe，仅停在付款页不支付", flush=True)

            await _print_summary(page, checkout_data, account_plan)
            if args.screenshot:
                try:
                    await page.screenshot(path=args.screenshot, full_page=True)
                    print(f"SCREENSHOT {args.screenshot}", flush=True)
                except Exception as exc:
                    print(f"SCREENSHOT_FAILED {type(exc).__name__}", flush=True)
            print(f"KEEP_OPEN {args.keep_open}s", flush=True)
            await page.wait_for_timeout(max(0, args.keep_open) * 1000)
        finally:
            await context.close()
            await browser.close()


def main() -> None:
    parser = argparse.ArgumentParser(description="ChatGPT checkout 自动订阅流程")
    parser.add_argument("--account-file", help="email----password----TOTP 文件")
    parser.add_argument("--account-line", help="email----password----TOTP")
    parser.add_argument(
        "--source-dir",
        default=os.getenv("ACCOUNT_IMPORT_AUTH_SOURCE", "/opt/any-auto-register"),
        help="any-auto-register 源码目录",
    )
    parser.add_argument("--proxy", help="http://user:pass@host:port；不填则直连")
    parser.add_argument("--promo", default="", help="促销 ID，例如 plus-3-months-50-pct-off")
    parser.add_argument("--country", default="US")
    parser.add_argument("--currency", default="USD")
    parser.add_argument("--checkout-url", help="复用已存在的 checkout URL，不新建订单")
    parser.add_argument(
        "--billing-api",
        choices=("usaddress", "geomock"),
        default="usaddress",
        help="账单地址来源（usaddress 用站点会话，无需 key；geomock 同源接口）",
    )
    parser.add_argument("--billing-state", default="DE", help="州代码，默认 DE")
    parser.add_argument("--billing-city", default="Dover", help="城市，默认 Dover")
    parser.add_argument(
        "--billing-api-url",
        default=DEFAULT_BILLING_API_URL,
        help="Bearer key 模式的地址接口",
    )
    parser.add_argument(
        "--billing-api-key",
        default=os.getenv("BILLING_API_KEY", ""),
        help="地址接口 Bearer key（默认读 BILLING_API_KEY）",
    )
    parser.add_argument(
        "--mock-billing-url",
        help=f"geomock 地址接口（默认 {DEFAULT_MOCK_BILLING_URL}）",
    )
    parser.add_argument("--fill-billing", action="store_true", help="填写测试账单字段")
    parser.add_argument("--card-file", help="测试卡 JSON")
    parser.add_argument("--no-fill", action="store_true", help="只停在付款页，不填卡")
    parser.add_argument(
        "--subscribe",
        action="store_true",
        help="填卡后点击 Subscribe 完成支付（不加则只停在付款页）",
    )
    parser.add_argument("--headless", action="store_true")
    parser.add_argument(
        "--allow-tax",
        action="store_true",
        help="税费不为 0 时仍提交（默认要求 Estimated tax = 0）",
    )
    parser.add_argument("--browser-channel", default="chrome")
    parser.add_argument("--keep-open", type=int, default=30)
    parser.add_argument("--screenshot")
    args = parser.parse_args()
    if not args.account_file and not args.account_line:
        parser.error("需要 --account-file 或 --account-line")
    asyncio.run(_run(args))


if __name__ == "__main__":
    main()
