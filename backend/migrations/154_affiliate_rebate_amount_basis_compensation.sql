-- Compensate historical affiliate rebates that were calculated from the paid
-- amount instead of the credited balance amount.
--
-- This is append-only: it keeps the original accrue ledger rows, adds one
-- AFFILIATE_REBATE_COMPENSATION audit marker per order, and inserts a positive
-- delta accrue row. Re-running is safe because the audit marker is the
-- idempotency key.

WITH settings_cfg AS (
    SELECT LEAST(
        GREATEST(
            COALESCE((
                SELECT FLOOR(value::numeric)::integer
                FROM settings
                WHERE key = 'affiliate_rebate_freeze_hours'
                  AND value ~ '^[0-9]+(\.[0-9]+)?$'
                LIMIT 1
            ), 0),
            0
        ),
        720
    ) AS freeze_hours
),
existing_rebates AS (
    SELECT ual.source_order_id AS order_id,
           ual.user_id AS inviter_id,
           ual.source_user_id AS invitee_id,
           SUM(ual.amount)::numeric AS rebate_amount,
           MIN(ual.created_at) AS first_rebate_at
    FROM user_affiliate_ledger ual
    WHERE ual.action = 'accrue'
      AND ual.source_order_id IS NOT NULL
      AND ual.source_user_id IS NOT NULL
    GROUP BY ual.source_order_id, ual.user_id, ual.source_user_id
),
candidates AS (
    SELECT er.order_id,
           er.inviter_id,
           er.invitee_id,
           er.rebate_amount,
           po.amount::numeric AS order_amount,
           po.pay_amount::numeric AS pay_amount,
           ROUND((er.rebate_amount / NULLIF(po.pay_amount::numeric, 0)), 12) AS historical_rate,
           ROUND(
               (po.amount::numeric * (er.rebate_amount / NULLIF(po.pay_amount::numeric, 0))) - er.rebate_amount,
               8
           ) AS compensation_amount,
           CASE
               WHEN sc.freeze_hours > 0
                    AND er.first_rebate_at + make_interval(hours => sc.freeze_hours) > NOW()
               THEN er.first_rebate_at + make_interval(hours => sc.freeze_hours)
               ELSE NULL::timestamptz
           END AS frozen_until
    FROM existing_rebates er
    JOIN payment_orders po ON po.id = er.order_id AND po.user_id = er.invitee_id
    JOIN payment_audit_logs applied
      ON applied.order_id = po.id::text
     AND applied.action = 'AFFILIATE_REBATE_APPLIED'
    JOIN user_affiliates inviter_aff ON inviter_aff.user_id = er.inviter_id
    CROSS JOIN settings_cfg sc
    WHERE po.order_type = 'balance'
      AND po.status = 'COMPLETED'
      AND po.amount > 0
      AND po.pay_amount > 0
      AND er.rebate_amount > 0
      AND ROUND(
          (po.amount::numeric * (er.rebate_amount / NULLIF(po.pay_amount::numeric, 0))) - er.rebate_amount,
          8
      ) > 0
      AND NOT EXISTS (
          SELECT 1
          FROM payment_audit_logs existing
          WHERE existing.order_id = po.id::text
            AND existing.action = 'AFFILIATE_REBATE_COMPENSATION'
      )
),
claimed AS (
    INSERT INTO payment_audit_logs (order_id, action, detail, operator, created_at)
    SELECT c.order_id::text,
           'AFFILIATE_REBATE_COMPENSATION',
           json_build_object(
               'reason', 'compensate affiliate rebate to credited amount basis',
               'orderAmount', c.order_amount,
               'payAmount', c.pay_amount,
               'originalRebateAmount', c.rebate_amount,
               'historicalRate', c.historical_rate,
               'compensationAmount', c.compensation_amount,
               'frozenUntil', c.frozen_until
           )::text,
           'system',
           NOW()
    FROM candidates c
    ON CONFLICT (order_id, action) DO NOTHING
    RETURNING order_id::bigint AS order_id
),
compensations AS (
    SELECT c.*
    FROM candidates c
    JOIN claimed cl ON cl.order_id = c.order_id
),
updated_affiliates AS (
    UPDATE user_affiliates ua
    SET aff_quota = ua.aff_quota + grouped.available_amount,
        aff_frozen_quota = ua.aff_frozen_quota + grouped.frozen_amount,
        aff_history_quota = ua.aff_history_quota + grouped.total_amount,
        updated_at = NOW()
    FROM (
        SELECT inviter_id,
               SUM(CASE WHEN frozen_until IS NULL THEN compensation_amount ELSE 0 END) AS available_amount,
               SUM(CASE WHEN frozen_until IS NOT NULL THEN compensation_amount ELSE 0 END) AS frozen_amount,
               SUM(compensation_amount) AS total_amount
        FROM compensations
        GROUP BY inviter_id
    ) grouped
    WHERE ua.user_id = grouped.inviter_id
    RETURNING ua.user_id
)
INSERT INTO user_affiliate_ledger (
    user_id,
    action,
    amount,
    source_user_id,
    source_order_id,
    frozen_until,
    created_at,
    updated_at
)
SELECT c.inviter_id,
       'accrue',
       c.compensation_amount,
       c.invitee_id,
       c.order_id,
       c.frozen_until,
       NOW(),
       NOW()
FROM compensations c
WHERE EXISTS (
    SELECT 1
    FROM updated_affiliates ua
    WHERE ua.user_id = c.inviter_id
);
