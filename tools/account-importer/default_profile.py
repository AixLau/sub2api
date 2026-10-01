"""Built-in import defaults, captured from erincpvdb125@gmail.com on 2026-09-27.

Only reusable settings are included. Account identity, tokens, status, and proxy
assignments are deliberately excluded. Runtime imports never fetch this account.
"""

from copy import deepcopy

DEFAULT_PROFILE_ID = 0
DEFAULT_PROFILE_NAME = "默认"

_DEFAULT_PROFILE = {
    "group_ids": [2, 5, 7, 15, 6, 8, 13],
    "settings": {
        "concurrency": 30,
        "priority": 1,
        "rate_multiplier": 1,
        "load_factor": 1000,
        "auto_pause_on_expired": True,
    },
    "extra": {
        "auto_reset_credit_enabled": False,
        "auto_reset_credit_5h_threshold": 1,
        "auto_reset_credit_7d_threshold": 1,
        "openai_long_context_billing_enabled": False,
        "openai_oauth_responses_websockets_v2_enabled": False,
        "openai_oauth_responses_websockets_v2_mode": "off",
    },
    "credentials": {
        "model_mapping": {
            "codex-auto-review": "codex-auto-review",
            "gpt-5.5": "gpt-5.5",
            "gpt-5.6-luna": "gpt-5.6-luna",
            "gpt-5.6-sol": "gpt-5.6-sol",
            "gpt-5.6-terra": "gpt-5.6-terra",
            "gpt-6-astra": "gpt-6-astra",
            "gpt-6-luna": "gpt-6-luna",
            "gpt-6-sol": "gpt-6-sol",
            "gpt-6.1-sol": "gpt-6.1-sol",
            "gpt-reserve": "gpt-reserve",
        },
    },
}


def default_import_profile() -> dict:
    """Return a fresh copy so one import cannot mutate future defaults."""
    return deepcopy(_DEFAULT_PROFILE)
