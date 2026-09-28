"""Model allowlists and mappings copied from a selected reference account."""
from copy import deepcopy

MODEL_SETTING_KEYS = ("model_mapping", "compact_model_mapping")


def model_settings(credentials: dict | None, *, required: bool = True) -> dict:
    credentials = credentials or {}
    mapping = credentials.get("model_mapping")
    if required and not mapping:
        raise ValueError("参考账号未设置模型限制，请先设置模型限制或选择已配置的参考账号")
    result = {}
    for key in MODEL_SETTING_KEYS:
        value = credentials.get(key)
        if value is None or value == {}:
            continue
        if not isinstance(value, dict) or any(
            not isinstance(source, str) or not source.strip()
            or not isinstance(target, str) or not target.strip()
            for source, target in value.items()
        ):
            raise ValueError("模型限制配置无效，请检查参考账号的模型映射")
        result[key] = deepcopy(value)
    return result
