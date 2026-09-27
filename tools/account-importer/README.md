# Sub2API account importer

An independent administrator tool for importing OpenAI OAuth accounts from
Sub2API JSON or one email----password----TOTP-secret line. It can append
successfully imported accounts to a selected, already enabled plugin, preserving
its current account scope. Failed plugin updates remain visible separately from
account import status. Existing accounts are reauthorized without duplication.

The tool reads optional reference accounts, groups, usable 5x proxies, and enabled
plugins from Sub2API. The first, preselected option is 默认 (built-in profile ID 0).
Its non-secret configuration is hardcoded in default_profile.py from
erincpvdb125@gmail.com as captured on 2026-09-27; runtime imports never look up
that account by name or ID. Deleting or changing the source account does not
affect this preset. It contains 7 groups, 9 model rules, concurrency 30,
priority 1, rate multiplier 1, load factor 1000, and the captured billing flags.
Choosing another account explicitly uses that account's current configuration.
New accounts inherit the selected profile's model mappings (including compact model
mappings), concurrency, priority, groups, rate multiplier, load factor,
and supported billing settings on import. Uploaded OAuth tokens and identity stay
with the imported account. An explicit group selection overrides inherited groups;
reimporting an existing account also enables it and selects a 5x proxy when available.
Other existing account configuration is preserved.

The 7-day monitor uses the existing account /usage API for both utilization and
window account-billed cost (including the account multiplier, in USD). Estimated
total quota is window account cost / (utilization / 100), matching the main
account page. It is an estimate, not an official fixed monetary allowance. Missing
or zero samples and expired windows are not estimated; usage query failures clear
financial values until the next successful poll.

Imports use active status with scheduling enabled, regardless
of the reference account status or scheduling flag. Each account independently
selects a random active, unexpired proxy named exactly 5x from the full proxy
list. If no eligible 5x proxy exists, imports and credential reauthorization connect
directly without a proxy. Reimporting or reauthorizing an existing account in this
case explicitly clears its previous proxy assignment. Proxy-list API failures
still surface as errors rather than being treated as an empty pool.
Reference/uploaded proxy assignments and uploaded proxy definitions are ignored;
other reference settings and the group override behavior are preserved.

Original import credentials and jobs are encrypted with Fernet and saved in a
separate logical database of the existing Sub2API Redis instance. Jobs and
one-click reauthorization survive importer restarts. Superseded jobs stop
monitoring and have their active stored credentials cleared.
The login adapter uses the pinned any-auto-register revision recorded in the
Dockerfile; upstream source is fetched at image build time, not vendored here.
No third-party login service is used. Account login may still require mailbox
verification or fail according to the upstream authentication response.

## Container deployment

Build only this tool, without rebuilding the gateway or running unrelated tests:

~~~sh
docker build --network=host -t sub2api-account-importer:VERSION tools/account-importer
~~~

Copy compose.yaml into a separate deployment directory, for example
/opt/sub2api-account-importer. Put the existing Sub2API administrator API key
in secrets/admin-api-key using a protected terminal or secret-management tool.
Do not put it in command arguments, Git, Docker build arguments, or image layers.
The secret file must be readable by container UID 10001 (owner 10001, mode 0400);
keep its parent directory root-owned with mode 0700.

Create a private .env next to the Compose file:

~~~dotenv
ACCOUNT_IMPORT_IMAGE=sub2api-account-importer:VERSION
SUB2API_BASE_URL=http://sub2api:8080
SUB2API_DOCKER_NETWORK=YOUR_EXISTING_SUB2API_NETWORK
SUB2API_REDIS_DB=0
ACCOUNT_IMPORT_REDIS_HOST=redis
ACCOUNT_IMPORT_REDIS_PORT=6379
ACCOUNT_IMPORT_REDIS_DB=1
~~~

~~~sh
docker compose up -d --wait
~~~

The importer joins the existing gateway Docker network and publishes only
127.0.0.1:8765 on the host. No Redis container or public Redis port is created.
Run one importer process: Redis persists jobs, while the active monitor tasks and
CSRF token belong to that process. The container runs as UID 10001 with a
read-only filesystem and a bounded temporary directory.

Before starting, verify the gateway database and choose an unused, different
Redis database. Startup rejects identical database numbers. The importer only
uses the hash sub2api-account-importer:v1:jobs in its selected database; it does
not flush databases or change the shared Redis configuration. Database separation
is logical isolation, not a security boundary against Redis administrators.
The shared instance's existing RDB/AOF persistence and backups still apply.

Generate a Fernet key on the server into secrets/importer-encryption-key, readable
by UID 10001 with mode 0400, without printing it or putting it in the image/Git.
Keep this same key across deployments and back it up separately from Redis data.
A missing/wrong key or unavailable Redis fails startup rather than silently
falling back to memory or replacing saved records. For authenticated Redis,
mount its password as a secret and set ACCOUNT_IMPORT_REDIS_PASSWORD_FILE.

Import data is saved before account login starts. Restart resumes continuous
monitoring, including previously completed monitoring jobs. Interrupted imports
without an account ID require an explicit one-click retry using the stored
first-import credentials.

## HTTPS and source IP restriction

Import Caddyfile.snippet inside the existing HTTPS site's block. Configure
ACCOUNT_IMPORT_ALLOWED_IP in Caddy's environment or replace that placeholder
with the exact allowed source IP in the server's private copy. Validate the
complete Caddy configuration before reloading it.

The browser entry point is https://YOUR_DOMAIN/account-import/. All page,
asset, health and API paths under this prefix require the allowed peer IP.
Forwarded headers do not bypass the allowlist. A CDN or a different VPN exit
will be rejected; connect directly with the authorized exit IP. Other gateway
routes retain their existing behavior. Do not expose port 8765 publicly.

The allowlist is the administrative access boundary: anyone using that allowed
exit IP can reach this tool. Do not relax it to a shared or public proxy address.
Test an allowed request, a denied request, and a spoofed forwarded-IP request
before considering deployment complete.

## Operations

- Health: curl -f http://127.0.0.1:8765/healthz on the server.
- State: docker compose ps from the importer deployment directory.
- Logs: docker compose logs --tail=50 importer (request-body logging is disabled).
- Update: build a new versioned image, set ACCOUNT_IMPORT_IMAGE, then run
  docker compose up -d --wait. Retain the prior image for rollback.
- API key rotation: replace the server secret file, then recreate the importer
  container. The application reads this secret only at startup.
- Plugin scope updates use the existing replacement API with a fresh read and
  union. Avoid simultaneous edits of the same plugin from other admin clients.
- Keep accounts/passwords/TOTP secrets, downloaded OAuth files and deployment
  secrets out of this directory and Git.

## Focused checks

From the repository root with Python dependencies installed:

~~~sh
PYTHONPATH=tools/account-importer:tools/account-importer/checks \
  python -m unittest discover -s tools/account-importer/checks -p 'test_*.py'
node --check tools/account-importer/static/app.js
~~~

For local development set SUB2API_BASE_URL, SUB2API_ADMIN_API_KEY_FILE (or
SUB2API_ADMIN_API_KEY) and ACCOUNT_IMPORT_AUTH_SOURCE to the pinned checkout,
plus the Redis variables above and ACCOUNT_IMPORT_ENCRYPTION_KEY_FILE, then run
python tools/account-importer/web_service.py. Production storage is mandatory;
only tests inject an in-memory fake. Set IMPORTER_REDIS_TEST_PORT to an isolated
local test Redis port to run the real DB 0/DB 1 isolation check.


### 手动重新授权

- 账号资料导入：任务「操作」列提供「手动重新授权」。可在 401 后主动触发；直接使用首次导入的「邮箱----密码----2FA 密钥」，不再要求二次输入；持续监控期间仍可一键重试。手动授权不受自动重试 3 次或冷却时间限制。
- JSON 导入：可填写当前账号的 RT，服务经 Sub2API 刷新接口换取授权并校验账号身份后写回；也可重新上传只包含同名账号的 JSON。无法核实身份或身份不匹配时不会写入。
- 授权后继续持续监控，保留账号 ID、分组和插件绑定。正在授权时不接受重复操作；已被新导入任务接管的旧任务不可操作。
- 手动提交均要求 CSRF 校验。密码、2FA 和 RT 不在任务列表中返回。账号资料与任务加密保存在单独的 Redis 数据库，服务重启后自动恢复，无需再次输入首次导入资料。RT 输入框在提交或关闭时清空。

## Continuous monitoring and model restrictions

Account state is polled every 30 seconds with no time limit, including JSON
accounts with authorization errors. Credential imports use their encrypted original
email/password/TOTP for automatic authorization on any remote account error or
invalid credentials. Each incident permits one initial authorization and up to
three retries, 60 seconds apart. After exhaustion the watcher keeps polling and
manual authorization remains available. A confirmed healthy status resets the
budget for a future incident. Restarts preserve the retry count and retry time.
Deleted accounts stop monitoring; superseded jobs remain retired. Manual
reauthorization failure does not stop monitoring.

Model restrictions are built into 默认 and required on any selected reference account, shown before
import, applied through the administrator account settings API, and read back for
verification. Reimporting with a reference also applies its model restrictions.
Authorization-only operations preserve existing model restrictions instead of
accepting model overrides from token files. The monitor shows the actual stored
model restrictions, not only the requested configuration.
