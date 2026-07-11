# Incremental Moderation Rollout Gates

Scrape `GET /api/v1/admin/risk-control/metrics` with an authenticated admin credential. The endpoint uses the normal admin authorization and compliance middleware. Do not expose it publicly.

Promotion from `observe` to `pre_block` is blocked if any query is unavailable or any gate below fails.

## Metrics

- `sub2api_moderation_requests_total{mode,cache_state,aggregate}`
- `sub2api_moderation_request_duration_seconds{mode,cache_state}`
- `sub2api_moderation_chunks`
- `sub2api_moderation_cache_operations_total{operation,result}`
- `sub2api_moderation_provider_calls_total{result}`
- `sub2api_moderation_batch_events_total{event}`
- `sub2api_moderation_correlation_total{result}`
- `sub2api_moderation_forwarded_evidence_total{state}`
- `sub2api_moderation_forced_fresh_total{result}`
- `sub2api_moderation_oldest_pending_review_seconds`
- `sub2api_moderation_confirmed_high_severity_misses_total`

Every label is a server-owned fixed enum. Request IDs, user IDs, models, URLs, risk text, API keys, and HMAC values are not labels.

## Promotion Queries

Seven days and at least 10,000 complete requests:

```promql
sum(increase(sub2api_moderation_requests_total{aggregate=~"pass|review|reject"}[7d])) >= 10000
```

At least 1,000 successful forced-fresh comparisons and 100% equivalence. Provider failures are excluded from the denominator and must be graphed separately:

```promql
sum(increase(sub2api_moderation_forced_fresh_total{result=~"equivalent|mismatch"}[7d])) >= 1000
and
sum(increase(sub2api_moderation_forced_fresh_total{result="mismatch"}[7d])) == 0
```

```promql
sum(increase(sub2api_moderation_forced_fresh_total{result="provider_error"}[24h]))
```

No unsafe evidence forwarded:

```promql
sum(increase(sub2api_moderation_forwarded_evidence_total{state=~"review|reject|error"}[7d])) == 0
```

Provider failures below 0.5% over 24 hours:

```promql
sum(increase(sub2api_moderation_provider_calls_total{result="error"}[24h]))
/
clamp_min(sum(increase(sub2api_moderation_provider_calls_total[24h])), 1) < 0.005
```

Incremental P95 below 1.5 seconds and cold P95 below 6 seconds:

```promql
histogram_quantile(0.95, sum by (le) (rate(sub2api_moderation_request_duration_seconds_bucket{cache_state="incremental"}[24h]))) < 1.5
```

```promql
histogram_quantile(0.95, sum by (le) (rate(sub2api_moderation_request_duration_seconds_bucket{cache_state="cold"}[24h]))) < 6
```

No unresolved correlated review older than 24 hours and no confirmed high-severity miss:

```promql
sub2api_moderation_oldest_pending_review_seconds < 86400
and
increase(sub2api_moderation_confirmed_high_severity_misses_total[7d]) == 0
```

Correlation ID completeness at least 99.9%:

```promql
sum(increase(sub2api_moderation_correlation_total{result!="missing_id"}[7d]))
/
clamp_min(sum(increase(sub2api_moderation_correlation_total[7d])), 1) >= 0.999
```

## Rollback

Set moderation mode to `observe` or `off`. To disable reuse without disabling fresh moderation, set `pass_cache_enabled` to `false`. Provider credentials can then be disabled or rotated. Do not delete Redis data during rollback; scoped TTLs and HMAC key-version rotation make stale PASS entries unreachable.
