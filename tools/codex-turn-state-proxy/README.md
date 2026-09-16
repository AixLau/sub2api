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
codex exec --skip-git-repo-check -s read-only \
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

## Full HTTP capture

For comparing provider behavior, add `-capture-dir /path/to/new-capture-dir`.
The parent must exist and the capture directory must not already exist. Each
request gets its own directory with:

| File | Contents |
| --- | --- |
| `request.json` | Destination, method, headers after auth/URL rewriting, content length, and time |
| `request.wire-headers.jsonl` | Transport-written headers, including Host/Content-Length or HTTP/2 pseudo-headers |
| `request.body` | Exact outbound HTTP entity body |
| `response.json` | Status, protocol, all upstream headers before proxy filtering, and time |
| `response.body` | Full response entity body, including SSE events and terminal response/usage metadata |
| `response.end.json` | Whether EOF was reached, captured byte count, trailers, and end time |

Authorization, cookies, and common API-key headers are redacted **only in the
capture files**, without changing forwarded values. Directories use mode 0700
and files use mode 0600. Bodies and request URLs are captured verbatim and may
contain private data; full capture is explicitly enabled with this flag.

Full capture buffers the request before sending it. Responses continue streaming
as they arrive. HTTP transfer framing is removed by Go, but Content-Encoding is
retained: decode gzip/zstd bodies according to the captured headers before
parsing JSON or SSE. `response.completed`, `response.incomplete`, or
`response.failed` events contain the final Response object when the upstream
provides it. An interrupted stream may have no final usage, so check both the
terminal event and `response.end.json`.

WebSocket captures include only handshake metadata, without response body/end
files. This observes traffic between this local proxy and the configured API
endpoint; it cannot observe that endpoint's internal requests to another service.

Run the focused checks without loading the backend module:

```bash
go test -race tools/codex-turn-state-proxy/main.go tools/codex-turn-state-proxy/main_test.go
```
