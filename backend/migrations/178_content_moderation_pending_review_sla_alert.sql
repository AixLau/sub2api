-- Alert when the human moderation review queue breaches its 24-hour SLA.
-- The evaluator keeps at most one firing event per rule and resolves it when
-- no pending record remains (the metric then returns zero).
INSERT INTO ops_alert_rules (
    name, description, enabled, metric_type, operator, threshold,
    window_minutes, sustained_minutes, severity, notify_email, cooldown_minutes,
    created_at, updated_at
) VALUES (
    '内容审核人工复核超时',
    '最老的待人工复核内容审计记录超过 24 小时时触发告警',
    true, 'content_moderation_pending_review_age_seconds', '>', 86400,
    1, 1, 'P1', true, 60, NOW(), NOW()
) ON CONFLICT (name) DO NOTHING;
