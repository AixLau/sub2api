package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/bridge"
	"github.com/google/uuid"
	hclog "github.com/hashicorp/go-hclog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const recentDiagnosticLimit = 50

type diagnosticEntry struct {
	StartedAt         string               `json:"started_at"`
	SessionID         string               `json:"session_id,omitempty"`
	ResponseID        string               `json:"response_id,omitempty"`
	ConfigRevision    string               `json:"config_revision,omitempty"`
	RetryReason       string               `json:"retry_reason,omitempty"`
	RouteHistory      []routeDecision      `json:"route_history,omitempty"`
	Time              string               `json:"time"`
	Event             string               `json:"event"`
	RequestID         string               `json:"request_id"`
	TraceID           string               `json:"trace_id"`
	AccountID         int64                `json:"account_id"`
	Model             string               `json:"model,omitempty"`
	Route             string               `json:"route,omitempty"`
	Reason            string               `json:"reason,omitempty"`
	Attempt           int                  `json:"attempt"`
	Stream            bool                 `json:"stream"`
	Status            int                  `json:"upstream_status,omitempty"`
	UpstreamRequestID string               `json:"upstream_request_id,omitempty"`
	DurationMS        int64                `json:"duration_ms"`
	ErrorCode         string               `json:"error_code,omitempty"`
	Tool              *bridge.Diagnostic   `json:"tool,omitempty"`
	RequestBody       *requestBodySnapshot `json:"request_body,omitempty"`
}

// Retain bounded failures with request body previews. Health is passive: no file reads,
// network calls or persistent writes are required to inspect recent failures.
type diagnosticRing struct {
	mu      sync.Mutex
	entries []diagnosticEntry
}

func (r *diagnosticRing) add(entry diagnosticEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) == recentDiagnosticLimit {
		copy(r.entries, r.entries[1:])
		r.entries = r.entries[:recentDiagnosticLimit-1]
	}
	r.entries = append(r.entries, entry)
}

func (r *diagnosticRing) snapshot() []diagnosticEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]diagnosticEntry{}, r.entries...)
}

func newDiagnosticLogger() hclog.Logger {
	// Capture the original stderr BEFORE go-plugin synchronizes os.Stderr.
	// Its subprocess log reader understands hclog's structured JSON protocol.
	return hclog.New(&hclog.LoggerOptions{Name: "bps.transport", JSONFormat: true, Level: hclog.Info, Output: os.Stderr})
}

type requestDiagnostics struct {
	p                                       *Plugin
	started                                 time.Time
	entry                                   diagnosticEntry
	requestBody                             []byte
	bodyAvailable, bodyComplete, bodyLogged bool
}
type requestDiagnosticsKey struct{}
type diagnosticStream struct {
	grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]
	ctx context.Context
}

func (s diagnosticStream) Context() context.Context { return s.ctx }

func (p *Plugin) startDiagnostics(ctx context.Context, start *pluginv1.ForwardRequestStart) (context.Context, *requestDiagnostics) {
	d := &requestDiagnostics{p: p, started: time.Now(), entry: diagnosticEntry{RequestID: safeLogID(start.RequestId), TraceID: uuid.NewString(), AccountID: start.AccountId}}
	d.entry.StartedAt = d.started.UTC().Format(time.RFC3339Nano)
	if values := metadata.ValueFromIncomingContext(ctx, pluginv1.ClientSessionIDMetadataKey); len(values) > 0 {
		d.entry.SessionID = safeLogID(values[0])
	}
	ctx = context.WithValue(ctx, requestDiagnosticsKey{}, d)
	ctx = bridge.WithDiagnosticObserver(ctx, func(tool bridge.Diagnostic) {
		previous := d.entry.ErrorCode
		d.entry.ErrorCode = "TOOL_BRIDGE_CALL_INVALID"
		if tool.Stage == "upstream_tool_capability" {
			d.entry.ErrorCode = "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE"
		}
		d.entry.Tool = &tool
		d.write("bps.tool_rejected", hclog.Warn)
		d.entry.ErrorCode = previous
		d.entry.Tool = nil
	})
	return ctx, d
}

func diagnosticsFrom(ctx context.Context) *requestDiagnostics {
	d, _ := ctx.Value(requestDiagnosticsKey{}).(*requestDiagnostics)
	return d
}

func (d *requestDiagnostics) body(body []byte, complete bool) {
	// Forward owns these immutable original bytes until finish. Never replace
	// them with the rewritten BPS request or a response/tool payload.
	d.requestBody = body
	d.bodyAvailable, d.bodyComplete = body != nil || complete, complete
	var meta struct {
		Model  string
		Stream bool
	}
	_ = json.Unmarshal(body, &meta)
	d.entry.Model, d.entry.Stream = safeLogID(meta.Model), meta.Stream
}

func (d *requestDiagnostics) route(route, reason string) {
	d.entry.Route, d.entry.Reason = route, reason
	d.entry.RouteHistory = append(d.entry.RouteHistory, routeDecision{
		Time: time.Now().UTC().Format(time.RFC3339Nano), Route: route, Reason: reason, Attempt: d.entry.Attempt + 1,
	})
	d.write("bps.route_selected", hclog.Info)
}

func (d *requestDiagnostics) retry(reason string) {
	d.entry.RetryReason = safeLogID(reason)
	d.write("bps.upstream_retry", hclog.Info)
}

func (d *requestDiagnostics) attempt() {
	d.entry.Attempt++
	d.entry.Status, d.entry.UpstreamRequestID = 0, ""
}

func (d *requestDiagnostics) upstream(response *http.Response) {
	d.entry.Status = response.StatusCode
	d.entry.UpstreamRequestID = safeLogID(response.Header.Get("X-Request-ID"))
	d.write("bps.upstream_response", hclog.Info)
}

func (d *requestDiagnostics) fail(code string) {
	if d == nil || d.entry.ErrorCode != "" {
		return
	}
	d.entry.ErrorCode = safeLogID(code)
	d.write("bps.request_failed", hclog.Warn)
}

func (d *requestDiagnostics) finish(err error) {
	if err != nil {
		d.fail("PLUGIN_RPC_FAILED")
	}
	if d.entry.Status >= 400 {
		d.fail("UPSTREAM_HTTP_ERROR")
	}
	d.write("bps.request_finished", hclog.Info)
}

func (d *requestDiagnostics) write(event string, level hclog.Level) {
	if level >= hclog.Warn {
		d.captureRequestBody()
	}
	entry := d.entry
	entry.RouteHistory = append([]routeDecision(nil), entry.RouteHistory...)
	entry.Event, entry.Time, entry.DurationMS = event, time.Now().UTC().Format(time.RFC3339Nano), time.Since(d.started).Milliseconds()
	if event == "bps.request_finished" {
		d.p.recentRequests.add(entry)
	}
	if level >= hclog.Warn {
		d.p.recentDiagnostics.add(entry)
	}
	// Fields remain structured through go-plugin into the host's slog sink.
	d.p.diagnosticLogger.Log(level, event, "plugin_id", PluginID, "plugin_version", PluginVersion,
		"request_id", entry.RequestID, "trace_id", entry.TraceID, "account_id", entry.AccountID,
		"session_id", entry.SessionID, "response_id", entry.ResponseID, "started_at", entry.StartedAt,
		"config_revision", entry.ConfigRevision, "route_history", entry.RouteHistory, "retry_reason", entry.RetryReason,
		"model", entry.Model, "route", entry.Route, "reason", entry.Reason, "attempt", entry.Attempt,
		"stream", entry.Stream, "upstream_status", entry.Status, "upstream_request_id", entry.UpstreamRequestID,
		"duration_ms", entry.DurationMS, "error_code", entry.ErrorCode, "tool", entry.Tool)
	if level >= hclog.Warn {
		d.logRequestBody()
	}
}

func safeLogID(value string) string {
	if len(value) > 128 {
		return "<redacted>"
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' {
			continue
		}
		return "<redacted>"
	}
	return value
}

func (d *requestDiagnostics) toolFeedback() {
	d.write("bps.tool_feedback", hclog.Info)
}
