#!/usr/bin/env python3
"""批量登录：并发协议登录，每个账号一个独立浏览器上下文（窗口）。"""

from __future__ import annotations

import argparse
import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from auto_recharge import _cookies_for_playwright, _login, _masked_email, _normalize_proxy, _proxy_settings


async def main() -> None:
    parser = argparse.ArgumentParser(description="批量登录 ChatGPT 账号")
    parser.add_argument("--account-file", required=True)
    parser.add_argument("--source-dir", default="/tmp/any-auto-register")
    parser.add_argument("--proxy")
    parser.add_argument("--concurrency", type=int, default=2)
    parser.add_argument("--keep-open", type=int, default=600)
    parser.add_argument("--screenshot-dir", default="")
    args = parser.parse_args()

    lines = [
        line.strip()
        for line in Path(args.account_file).expanduser().read_text(encoding="utf-8").splitlines()
        if line.strip()
    ]
    if not lines:
        raise SystemExit("账号文件为空")

    proxy = _normalize_proxy(args.proxy) if args.proxy else None
    source_dir = Path(args.source_dir).expanduser().resolve()
    semaphore = asyncio.Semaphore(max(1, args.concurrency))

    async def login_one(line: str) -> dict:
        async with semaphore:
            try:
                flow, token, email, cookies = await asyncio.to_thread(
                    _login, line, source_dir, proxy
                )
                print(f"ACCOUNT_OK {_masked_email(email)} cookies={len(cookies)}", flush=True)
                return {
                    "ok": True,
                    "email": email,
                    "cookies": cookies,
                }
            except Exception as exc:
                print(
                    f"ACCOUNT_FAIL {type(exc).__name__}: {str(exc)[:120]}",
                    flush=True,
                )
                return {"ok": False, "error": f"{type(exc).__name__}: {exc}"}

    results = await asyncio.gather(*(login_one(line) for line in lines))
    ok_accounts = [item for item in results if item.get("ok")]
    print(
        f"LOGIN_SUMMARY ok={len(ok_accounts)}/{len(results)}",
        flush=True,
    )
    if not ok_accounts:
        return

    from playwright.async_api import async_playwright

    screenshot_dir = Path(args.screenshot_dir) if args.screenshot_dir else None
    if screenshot_dir:
        screenshot_dir.mkdir(parents=True, exist_ok=True)

    async with async_playwright() as playwright:
        launch_kwargs = {
            "headless": False,
            "proxy": _proxy_settings(proxy),
            "args": ["--disable-blink-features=AutomationControlled"],
        }
        if _proxy_settings(proxy) is None:
            launch_kwargs["args"] += ["--no-proxy-server"]
        launch_kwargs["args"] += ["--start-maximized"]
        try:
            browser = await playwright.chromium.launch(channel="chrome", **launch_kwargs)
        except Exception:
            browser = await playwright.chromium.launch(**launch_kwargs)

        pages = []
        for index, account in enumerate(ok_accounts, start=1):
            context = await browser.new_context(
                locale="en-US",
                no_viewport=True,
                ignore_https_errors=True,
                user_agent=(
                    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
                    "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
                ),
            )
            added = 0
            for cookie in account["cookies"]:
                try:
                    await context.add_cookies([cookie])
                    added += 1
                except Exception:
                    continue
            page = await context.new_page()
            try:
                await page.goto("https://chatgpt.com/", wait_until="commit", timeout=120000)
                await page.wait_for_timeout(8000)
                state = ""
                for _ in range(5):
                    try:
                        state = await page.evaluate(
                            "() => (document.body ? (document.body.innerText || '') : '')"
                            ".replace(/\\s+/g, ' ').slice(0, 160)"
                        )
                    except Exception:
                        state = ""
                    if state:
                        break
                    await page.wait_for_timeout(2000)
                print(
                    f"BROWSER_READY #{index} {_masked_email(account['email'])} "
                    f"cookies={added} page='{state[:80]}'",
                    flush=True,
                )
                if screenshot_dir:
                    shot = screenshot_dir / f"login_{index}_{_masked_email(account['email']).replace('*','x')}.png"
                    await page.screenshot(path=str(shot))
                    print(f"SCREENSHOT {shot}", flush=True)
            except Exception as exc:
                print(f"BROWSER_ERROR #{index} {type(exc).__name__}: {str(exc)[:100]}", flush=True)
            pages.append(page)

        if args.keep_open < 0:
            print(f"KEEP_OPEN infinite ({len(pages)} windows)", flush=True)
            await asyncio.Event().wait()
        else:
            print(f"KEEP_OPEN {args.keep_open}s ({len(pages)} windows)", flush=True)
            await asyncio.sleep(max(0, args.keep_open))

        for page in pages:
            try:
                await page.context.close()
            except Exception:
                pass
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
