#!/usr/bin/env python3
"""定位 Plus 订阅入口：截图 + dump 头像菜单 + 探测 payments/checkout API。"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from auto_recharge import _cookies_for_playwright, _login, _proxy_settings

SHOT_DIR = Path("/private/var/folders/j_/wj8mmnfx46zccr8bvxj1wklh0000gn/T/opencode")


async def main() -> None:
    parser = argparse.ArgumentParser(description="ChatGPT 订阅入口诊断")
    parser.add_argument("--account-line", required=True)
    parser.add_argument("--proxy")
    parser.add_argument("--source-dir", default="/tmp/any-auto-register")
    args = parser.parse_args()

    source_dir = Path(args.source_dir).resolve()
    flow, _token, email, cookies = _login(args.account_line, source_dir, args.proxy)
    print(f"LOGIN_OK email={email[:2]}*** cookies={len(cookies)}", flush=True)

    from playwright.async_api import async_playwright

    async with async_playwright() as playwright:
        launch_kwargs = {
            "headless": False,
            "proxy": _proxy_settings(args.proxy),
            "args": ["--disable-blink-features=AutomationControlled"],
        }
        try:
            browser = await playwright.chromium.launch(channel="chrome", **launch_kwargs)
        except Exception:
            browser = await playwright.chromium.launch(**launch_kwargs)
        context = await browser.new_context(
            locale="en-US",
            viewport={"width": 1365, "height": 1000},
            ignore_https_errors=True,
            user_agent=(
                "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
                "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
            ),
        )
        for cookie in cookies:
            try:
                await context.add_cookies([cookie])
            except Exception:
                continue
        page = await context.new_page()

        await page.goto("https://chatgpt.com/", wait_until="commit", timeout=120000)
        await page.wait_for_timeout(8000)
        await page.screenshot(path=str(SHOT_DIR / "pricing_debug_1_home.png"))

        # 点头像按钮，dump 弹出的菜单
        clicked = None
        for sel in (
            '[data-testid="profile-button"]',
            'button[aria-label*="profile" i]',
            'button:has-text("Free")',
            'button:has-text("Plus")',
        ):
            try:
                await page.locator(sel).first.click(timeout=3000)
                clicked = sel
                break
            except Exception:
                continue
        print(f"PROFILE_CLICK {clicked}", flush=True)
        await page.wait_for_timeout(2500)
        if clicked:
            await page.screenshot(path=str(SHOT_DIR / "pricing_debug_2_menu.png"))
        menu = await page.evaluate(
            """() => {
                const visible = (el) => {
                    const r = el.getBoundingClientRect();
                    const s = getComputedStyle(el);
                    return r.width > 0 && r.height > 0 && s.display !== 'none' && s.visibility !== 'hidden';
                };
                const norm = (s) => (s || '').replace(/\\s+/g, ' ').trim();
                const out = {};
                for (const sel of ['[role="menu"]', '[role="dialog"]', '[data-testid*="menu" i]', '[class*="popover" i]']) {
                    out[sel] = [...document.querySelectorAll(sel)].filter(visible)
                        .map((el) => norm(el.innerText).slice(0, 500));
                }
                return out;
            }"""
        )
        print("MENU_DUMP " + json.dumps(menu, ensure_ascii=False)[:2500], flush=True)
        try:
            await page.keyboard.press("Escape")
            await page.wait_for_timeout(500)
        except Exception:
            pass

        # 试直达路由
        for url in ("https://chatgpt.com/upgrade", "https://chatgpt.com/#pricing"):
            try:
                await page.goto(url, wait_until="commit", timeout=60000)
                await page.wait_for_timeout(6000)
                print(f"ROUTE {url} -> {page.url}", flush=True)
                await page.screenshot(path=str(SHOT_DIR / "pricing_debug_3_route.png"))
            except Exception as exc:
                print(f"ROUTE_ERR {url} {type(exc).__name__}", flush=True)

        # 探测 checkout API：UI 点击最终就是 POST 这个接口
        payloads = [
            {},
            {"plan": "plus"},
            {"plan": "plus", "interval": "month"},
            {"processor_entity": "openai_llc"},
            {"plan": "plus", "processor_entity": "openai_llc"},
        ]
        probes = await page.evaluate(
            """async (payloads) => {
                const out = [];
                for (const p of payloads) {
                    try {
                        const r = await fetch('/backend-api/payments/checkout', {
                            method: 'POST',
                            headers: {'content-type': 'application/json'},
                            body: JSON.stringify(p),
                        });
                        const t = await r.text();
                        out.push({payload: p, status: r.status, body: t.slice(0, 400)});
                    } catch (e) {
                        out.push({payload: p, error: String(e)});
                    }
                }
                return out;
            }""",
            payloads,
        )
        print("CHECKOUT_PROBE " + json.dumps(probes, ensure_ascii=False)[:4000], flush=True)

        await context.close()
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
