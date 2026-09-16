# Codex turn-state capture proxy

A local reverse proxy that records `X-Codex-Turn-State` from incoming requests
and upstream responses as JSON lines. It forwards HTTP/SSE streams and WebSocket
upgrades using Go's `net/http/httputil.ReverseProxy`, with no external dependencies.

From the repository root (Go 1.21+):

```bash
go run tools/codex-turn-state-proxy/main.go \
  -upstream https://api.example.com
```

The listener defaults to `127.0.0.1:18080`. Point the client's Responses base URL
at `http://127.0.0.1:18080/v1`. Paths are appended to the upstream base path, so
use an upstream URL without `/v1` when the client already sends `/v1`.

For Codex with an existing provider named `custom`, override its URL for one run:

```bash
codex exec --ephemeral --skip-git-repo-check -s read-only \
  -c 'model_providers.custom.base_url="http://127.0.0.1:18080/v1"' \
  'Reply only OK. Do not use tools.'
```

Normally the incoming `Authorization` header is forwarded unchanged. If a
switching proxy has replaced the client's credential with a placeholder, set
`SUB2API_API_KEY` in the capture proxy's environment to the real upstream key.
Only the outgoing authorization is replaced; the key is never logged.

Logs appear on stdout; startup and transport diagnostics go to stderr. For a
local capture file, use `umask 077` before redirecting stdout. Only the turn-state
header values and basic request/status information are recorded, never bodies,
cookies, or authorization. Turn-state values are printed in full for inspection.

```json
{"msg":"inbound_request","request_id":1,"method":"POST","path":"/v1/responses","present":false,"turn_state":null}
{"msg":"upstream_response","request_id":1,"status":200,"present":true,"turn_state":["example-opaque-state"]}
```

Actual records also include a timestamp and log level. `present: false` means
the header was absent; `present: true` with `[""]` means it was explicitly empty.
The same `request_id` joins both directions. A fresh turn can have no inbound
state. This proxy does not synthesize or inject turn state, and cannot guarantee
that the upstream returns it. For WebSocket, it observes the HTTP handshake
headers, not metadata inside frames. Stop the proxy with Ctrl+C.

Run the focused checks without loading the backend module:

```bash
go test -race tools/codex-turn-state-proxy/main.go tools/codex-turn-state-proxy/main_test.go
```
