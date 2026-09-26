# Opt-in test transport. Credentials stay in remote process memory and are sent
# only to the official BPS endpoint. Never print the SQL result or auth headers.
import json
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

request = json.load(sys.stdin)
email = request['email'].replace("'", "''")
query = """SELECT json_build_object(
 'token', a.credentials->>'access_token',
 'account_id', a.credentials->>'chatgpt_account_id',
 'proxy_protocol', p.protocol, 'proxy_host', p.host, 'proxy_port', p.port,
 'proxy_user', p.username, 'proxy_password', p.password)
FROM accounts a LEFT JOIN proxies p ON p.id=a.proxy_id
WHERE a.deleted_at IS NULL AND a.platform='openai' AND a.type='oauth'
 AND a.status='active' AND a.credentials->>'email'='%s';""" % email
db = subprocess.run(['docker', 'exec', '-i', 'sub2api-postgres', 'psql', '-X', '-U', 'sub2api', '-d', 'sub2api', '-At', '-v', 'ON_ERROR_STOP=1', '-c', query], capture_output=True, text=True)
if db.returncode != 0:
    raise SystemExit('account lookup failed')
rows = db.stdout.strip().splitlines()
if len(rows) != 1:
    raise SystemExit('expected exactly one active OAuth account')
identity = json.loads(rows[0])
if not identity.get('token') or not identity.get('account_id'):
    raise SystemExit('account has no usable authorization')
headers = {
 'Authorization': 'Bearer ' + identity['token'],
 'chatgpt-account-id': identity['account_id'],
 'x-openai-account-id': identity['account_id'],
 'x-basispoints-auth-mode': 'chatgpt',
 'Content-Type': 'application/json', 'Accept': 'text/event-stream',
 'User-Agent': 'codex-tui/0.158.0 (Ubuntu 22.4.0; x86_64) xterm-256color',
 'originator': 'codex-tui', 'version': '0.158.0', 'OpenAI-Beta': 'responses=experimental',
 'x-openai-internal-basispoints-client-agent-profile': 'excel',
 'x-openai-internal-basispoints-client-editor': 'excel',
 'x-openai-internal-basispoints-client-host': 'office',
 'x-openai-internal-basispoints-client-platform': 'excel',
 'x-openai-internal-basispoints-client-platform-class': 'PC',
 'x-openai-internal-basispoints-client-product': 'basispoints-excel-plugin',
 'x-openai-internal-basispoints-client-runtime': 'desktop',
 'x-openai-internal-basispoints-office-host': 'Excel',
 'x-openai-internal-basispoints-office-platform': 'PC',
 'x-stainless-runtime': 'browser:chrome', 'x-stainless-lang': 'js',
}
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

handlers = [NoRedirect()]
if identity.get('proxy_host'):
    if identity['proxy_protocol'] not in ('http', 'https'):
        raise SystemExit('this probe requires an HTTP account proxy')
    auth = ''
    if identity.get('proxy_user'):
        auth = urllib.parse.quote(identity['proxy_user'], safe='') + ':' + urllib.parse.quote(identity.get('proxy_password') or '', safe='') + '@'
    proxy = '%s://%s%s:%s' % (identity['proxy_protocol'], auth, identity['proxy_host'], identity['proxy_port'])
    handlers.append(urllib.request.ProxyHandler({'http': proxy, 'https': proxy}))
else:
    handlers.append(urllib.request.ProxyHandler({}))
opener = urllib.request.build_opener(*handlers)
req = urllib.request.Request('https://bps.openai.com/basispoints/api/responses', data=json.dumps(request['body']).encode(), headers=headers, method='POST')
try:
    with opener.open(req, timeout=150) as response:
        status = response.status
        body = response.read(8 * 1024 * 1024).decode('utf-8')
except urllib.error.HTTPError as error:
    status = error.code
    body = error.read(16384).decode('utf-8', errors='replace')
except Exception:
    raise SystemExit('upstream connection failed')
for secret in (identity.get('token'), identity.get('proxy_password')):
    if secret:
        body = body.replace(secret, '[redacted]')
print(json.dumps({'status': status, 'body': body}))
