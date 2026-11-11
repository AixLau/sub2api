#!/usr/bin/env python3
"""只读检查一个 ChatGPT checkout 是否带人工审核标记。

用法：
  python check_checkout.py \
    --account-file ~/.sub2api-importer/airy.dragon-9w@icloud.com.credentials \
    --checkout-url 'https://chatgpt.com/checkout/openai_llc/oaics_...'

只调用 mfa/checkout 查询接口，不创建订单，不付款。
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import sys
from pathlib import Path
from urllib.parse import urlsplit

IMPORTER = Path(__file__).resolve().parent
if str(IMPORTER) not in sys.path:
    sys.path.insert(0, str(IMPORTER))

import reset_2fa
from local_relogin import parse_account_line


def jwt_claims(token: str) -> dict:
    try:
        part = token.split(".")[1]
        part += "=" * (-len(part) % 4)
        return json.loads(base64.urlsafe_b64decode(part))
    except Exception:
        return {}


def parse_checkout(args: argparse.Namespace) -> tuple[str, str]:
    if args.checkout_url:
        path = urlsplit(args.checkout_url).path
        parts = [part for part in path.split("/") if part]
        if len(parts) >= 3 and parts[0] == "checkout":
            return parts[1], parts[2]
        raise SystemExit("checkout URL 应形如 https://chatgpt.com/checkout/openai_llc/oaics_...")
    if args.checkout_id:
        return args.processor_entity, args.checkout_id
    raise SystemExit("需要 --checkout-url 或 --checkout-id")


def main() -> None:
    parser = argparse.ArgumentParser(description="只读检查 ChatGPT checkout 审核标记")
    parser.add_argument("--account-file", help="email----password----totp 文件")
    parser.add_argument("--account-line", help="email----password----totp")
    parser.add_argument("--checkout-url", help="付款页 URL，含 oaics_...")
    parser.add_argument("--checkout-id", help="oaics_...")
    parser.add_argument("--processor-entity", default="openai_llc")
    parser.add_argument("--proxy")
    parser.add_argument("--retries", type=int, default=1)
    parser.add_argument(
        "--source-dir",
        default=os.getenv("ACCOUNT_IMPORT_AUTH_SOURCE", "/opt/any-auto-register"),
    )
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()

    if args.account_file:
        line = Path(args.account_file).expanduser().read_text(encoding="utf-8").strip()
    elif args.account_line:
        line = args.account_line.strip()
    else:
        raise SystemExit("需要 --account-file 或 --account-line")
    email, password, secret = parse_account_line(line)
    processor_entity, checkout_id = parse_checkout(args)

    reset_2fa.UPSTREAM = reset_2fa.load_upstream(Path(args.source_dir).expanduser().resolve())
    account = reset_2fa.Account(1, email, password, secret, line)
    flow, access_token = reset_2fa.login(account, args.proxy, args.retries)
    auth = jwt_claims(access_token).get("https://api.openai.com/auth") or {}
    account_id = str(auth.get("chatgpt_account_id") or "")

    headers = {
        "Authorization": f"Bearer {access_token}",
        "Accept": "application/json",
        "oai-language": "zh-CN",
        "originator": "Codex Browser",
        "x-openai-checkout-source": "chatgpt_web_merged",
        "x-openai-codex-window-type": "browser",
        "x-openai-web-frontend": "codex_webview",
        "Referer": "https://chatgpt.com/",
    }
    if account_id:
        headers["chatgpt-account-id"] = account_id

    url = f"https://chatgpt.com/backend-api/payments/checkout/{processor_entity}/{checkout_id}"
    response = flow.session.get(url, headers=headers, timeout=30)
    if response.status_code != 200:
        print(f"CHECKOUT_HTTP {response.status_code} {response.text[:300]}")
        raise SystemExit(1)
    data = response.json() or {}
    state = data.get("checkout_state") if isinstance(data.get("checkout_state"), dict) else {}
    total = state.get("total") if isinstance(state.get("total"), dict) else {}
    total_amount = total.get("total") if isinstance(total.get("total"), dict) else {}
    safe = {
        "checkout_session_id": data.get("checkout_session_id"),
        "processor_entity": data.get("processor_entity"),
        "checkout_ui_mode": data.get("checkout_ui_mode"),
        "plan_name": data.get("plan_name"),
        "promo_campaign": data.get("promo_campaign"),
        "payment_status": data.get("payment_status"),
        "status": data.get("status"),
        "requires_manual_approval": data.get("requires_manual_approval"),
        "canConfirm": state.get("canConfirm"),
        "total_minor_units": total_amount.get("minorUnitsAmount"),
        "total_amount": total_amount.get("amount"),
        "billing_details": data.get("billing_details"),
        "account_plan_type": auth.get("chatgpt_plan_type"),
    }

    account_response = flow.session.get(
        "https://chatgpt.com/backend-api/accounts/check/v4-2023-04-27",
        headers=headers,
        timeout=30,
    )
    if account_response.status_code == 200:
        accounts = (account_response.json() or {}).get("accounts") or {}
        for aid, item in accounts.items():
            acct = item.get("account") or {}
            entry = item.get("entitlements") or {}
            safe.setdefault("accounts", {})[aid[:8] + "..."] = {
                "plan_type": acct.get("plan_type"),
                "subscription_plan": entry.get("subscription_plan"),
            }

    will_review = bool(safe.get("requires_manual_approval"))
    if safe.get("payment_status") == "paid" or safe.get("account_plan_type") == "plus":
        verdict = "PAID_OR_PLUS"
    elif will_review:
        verdict = "MANUAL_REVIEW"
    else:
        verdict = "NO_MANUAL_APPROVAL_FLAG"
    safe["verdict"] = verdict

    if args.json:
        print(json.dumps(safe, ensure_ascii=False, indent=2))
    else:
        print(f"CHECKOUT {checkout_id}")
        print(f"REQUIRES_MANUAL_APPROVAL {safe.get('requires_manual_approval')}")
        print(f"PAYMENT_STATUS {safe.get('payment_status')}")
        print(f"STATUS {safe.get('status')}")
        print(f"CAN_CONFIRM {safe.get('canConfirm')}")
        print(f"TOTAL_MINOR_UNITS {safe.get('total_minor_units')}")
        print(f"PLAN_TYPE {safe.get('account_plan_type')}")
        print(f"VERDICT {verdict}")
        print(json.dumps(safe, ensure_ascii=False))


if __name__ == "__main__":
    main()
