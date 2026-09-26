package transport

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"unicode/utf8"

	hclog "github.com/hashicorp/go-hclog"
)

// Even JSON's worst-case six-byte escaping fits below go-plugin's 64 KiB
// stderr line limit. Health retains only the first part, never whole bodies.
const requestBodyPartBytes = 8 << 10

type requestBodySnapshot struct {
	Available        bool   `json:"available"`
	Complete         bool   `json:"complete"`
	Bytes            int    `json:"bytes"`
	SHA256           string `json:"sha256,omitempty"`
	Encoding         string `json:"encoding,omitempty"`
	Parts            int    `json:"parts,omitempty"`
	Preview          string `json:"preview"`
	PreviewTruncated bool   `json:"preview_truncated"`
}

func (d *requestDiagnostics) captureRequestBody() {
	if d.entry.RequestBody != nil {
		return
	}
	snapshot := &requestBodySnapshot{Available: d.bodyAvailable, Complete: d.bodyComplete, Bytes: len(d.requestBody)}
	d.entry.RequestBody = snapshot
	if !snapshot.Available {
		return
	}
	sum := sha256.Sum256(d.requestBody)
	snapshot.SHA256 = hex.EncodeToString(sum[:])
	snapshot.Encoding = "utf-8"
	if !utf8.Valid(d.requestBody) {
		snapshot.Encoding = "base64"
	}
	for rest, first := d.requestBody, true; first || len(rest) > 0; first = false {
		n := requestBodyPartLength(rest, snapshot.Encoding)
		if first {
			snapshot.Preview = encodeBodyPart(rest[:n], snapshot.Encoding)
			snapshot.PreviewTruncated = n < len(rest)
		}
		snapshot.Parts++
		rest = rest[n:]
	}
}

func requestBodyPartLength(body []byte, encoding string) int {
	n := min(len(body), requestBodyPartBytes)
	if encoding == "utf-8" {
		for n < len(body) && !utf8.RuneStart(body[n]) {
			n--
		}
	}
	return n
}

func encodeBodyPart(body []byte, encoding string) string {
	if encoding == "base64" {
		return base64.StdEncoding.EncodeToString(body)
	}
	return string(body)
}

func (d *requestDiagnostics) logRequestBody() {
	if d.bodyLogged || d.entry.RequestBody == nil || !d.entry.RequestBody.Available {
		return
	}
	d.bodyLogged = true
	snapshot := d.entry.RequestBody
	rest := d.requestBody
	for part := 1; part <= snapshot.Parts; part++ {
		n := requestBodyPartLength(rest, snapshot.Encoding)
		d.p.diagnosticLogger.Log(hclog.Warn, "bps.failed_request_body",
			"plugin_id", PluginID, "plugin_version", PluginVersion,
			"request_id", d.entry.RequestID, "trace_id", d.entry.TraceID, "account_id", d.entry.AccountID,
			"model", d.entry.Model, "route", d.entry.Route,
			"error_code", d.entry.ErrorCode, "request_body_bytes", snapshot.Bytes, "request_body_sha256", snapshot.SHA256,
			"request_body_complete", snapshot.Complete, "request_body_encoding", snapshot.Encoding,
			"part", part, "parts", snapshot.Parts, "request_body", encodeBodyPart(rest[:n], snapshot.Encoding))
		rest = rest[n:]
	}
}
