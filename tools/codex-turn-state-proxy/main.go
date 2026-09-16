// Command codex-turn-state-proxy observes Codex turn-state headers while
// forwarding HTTP, SSE, and WebSocket traffic to a fixed upstream.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const turnStateHeader = "X-Codex-Turn-State"

type requestIDKey struct{}

func newProxy(target *url.URL, apiKey, captureDir string, logger *slog.Logger) http.Handler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Preserve the client's encoding choice; don't add/decode gzip implicitly.
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = 2 * time.Minute

	var roundTripper http.RoundTripper = transport
	if captureDir != "" {
		roundTripper = &captureTransport{base: transport, dir: captureDir}
	}
	proxy := &httputil.ReverseProxy{
		Transport:     roundTripper,
		FlushInterval: -1,
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			if apiKey != "" {
				r.Out.Header.Set("Authorization", "Bearer "+apiKey)
			}
		},
		ModifyResponse: func(r *http.Response) error {
			// Also called for a 101 WebSocket upgrade, before tunneling frames.
			logger.Info("upstream_response",
				"request_id", r.Request.Context().Value(requestIDKey{}),
				"status", r.StatusCode,
				"present", len(r.Header.Values(turnStateHeader)) > 0,
				"turn_state", r.Header.Values(turnStateHeader),
			)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Don't log arbitrary transport error text: it can contain URLs.
			logger.Error("upstream_error",
				"request_id", r.Context().Value(requestIDKey{}),
				"error_type", fmt.Sprintf("%T", err),
			)
			http.Error(w, "upstream proxy error", http.StatusBadGateway)
		},
	}
	var sequence atomic.Uint64
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sequence.Add(1)
		logger.Info("inbound_request",
			"request_id", id,
			"method", r.Method,
			"path", r.URL.Path,
			"present", len(r.Header.Values(turnStateHeader)) > 0,
			"turn_state", r.Header.Values(turnStateHeader),
		)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
}

// captureTransport runs after the proxy rewrites the destination and auth, and
// before ReverseProxy removes hop-by-hop response headers. Bodies are saved as
// HTTP entity bytes (dechunked, but retaining Content-Encoding).
type captureTransport struct {
	base http.RoundTripper
	dir  string
}

func redactedHeaders(h http.Header) http.Header {
	out := h.Clone()
	for key := range out {
		switch strings.ToLower(key) {
		case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key", "x-goog-api-key", "x-auth-token", "x-access-token":
			out[key] = []string{"[REDACTED]"}
		}
	}
	return out
}

func writeCaptureJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	id := req.Context().Value(requestIDKey{})
	dir, err := os.MkdirTemp(t.dir, fmt.Sprintf("request-%v-", id))
	if err != nil {
		return nil, err
	}
	// Diagnostic mode buffers only the request, never the SSE response.
	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(req.Body)
		closeErr := req.Body.Close()
		if err = errors.Join(err, closeErr); err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	if err := os.WriteFile(filepath.Join(dir, "request.body"), body, 0600); err != nil {
		return nil, err
	}
	if err := writeCaptureJSON(filepath.Join(dir, "request.json"), map[string]any{
		"time": time.Now(), "request_id": id, "method": req.Method,
		"url": req.URL.String(), "host": req.Host, "headers": redactedHeaders(req.Header),
		"content_length": req.ContentLength, "transfer_encoding": req.TransferEncoding,
	}); err != nil {
		return nil, err
	}
	// Trace transport-written headers, including Host/Content-Length and HTTP/2
	// pseudo-headers. Header values are redacted before they reach disk.
	headerFile, err := os.OpenFile(filepath.Join(dir, "request.wire-headers.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer headerFile.Close()
	var headerMu sync.Mutex
	var headerErr error
	trace := &httptrace.ClientTrace{WroteHeaderField: func(key string, values []string) {
		headerMu.Lock()
		defer headerMu.Unlock()
		if headerErr == nil {
			headerErr = json.NewEncoder(headerFile).Encode(redactedHeaders(http.Header{key: values}))
		}
	}}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	resp, err := t.base.RoundTrip(req)
	headerMu.Lock()
	err = errors.Join(err, headerErr)
	headerMu.Unlock()
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		_ = writeCaptureJSON(filepath.Join(dir, "error.json"), map[string]any{"error_type": fmt.Sprintf("%T", err)})
		return nil, err
	}
	if err := writeCaptureJSON(filepath.Join(dir, "response.json"), map[string]any{
		"time": time.Now(), "request_id": id, "status": resp.StatusCode, "protocol": resp.Proto,
		"headers": redactedHeaders(resp.Header), "content_length": resp.ContentLength,
		"transfer_encoding": resp.TransferEncoding, "uncompressed": resp.Uncompressed,
		"websocket_handshake_only": resp.StatusCode == http.StatusSwitchingProtocols,
	}); err != nil {
		resp.Body.Close()
		return nil, err
	}
	// Keep the upgraded body's ReadWriteCloser intact; do not capture WS frames.
	if resp.StatusCode == http.StatusSwitchingProtocols {
		return resp, nil
	}
	f, err := os.OpenFile(filepath.Join(dir, "response.body"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	resp.Body = &captureBody{ReadCloser: resp.Body, file: f, onClose: func(complete bool, size int64) error {
		return writeCaptureJSON(filepath.Join(dir, "response.end.json"), map[string]any{
			"time": time.Now(), "complete": complete, "body_bytes": size, "trailers": redactedHeaders(resp.Trailer),
		})
	}}
	return resp, nil
}

type captureBody struct {
	io.ReadCloser
	file     *os.File
	complete atomic.Bool
	size     atomic.Int64
	onClose  func(bool, int64) error
	once     sync.Once
	closeErr error
}

func (b *captureBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		written, writeErr := b.file.Write(p[:n])
		b.size.Add(int64(written))
		if writeErr != nil {
			return n, writeErr
		}
	}
	if err == io.EOF {
		b.complete.Store(true)
	}
	return n, err
}

func (b *captureBody) Close() error {
	b.once.Do(func() {
		b.closeErr = errors.Join(b.ReadCloser.Close(), b.file.Close(), b.onClose(b.complete.Load(), b.size.Load()))
	})
	return b.closeErr
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18080", "loopback listen address")
	upstream := flag.String("upstream", "", "upstream base URL, e.g. https://api.example.com (without /v1 if the client sends /v1)")
	captureDir := flag.String("capture-dir", "", "optional new directory for full HTTP captures (bodies may contain private data)")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("-listen must use a loopback IP, e.g. 127.0.0.1:18080")
	}
	target, err := url.Parse(*upstream)
	if err != nil || (target.Scheme != "https" && target.Scheme != "http") || target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		log.Fatal("-upstream must be an HTTP(S) URL without credentials, query, or fragment")
	}
	apiKey := strings.TrimSpace(os.Getenv("SUB2API_API_KEY"))
	if *captureDir != "" {
		// Refuse existing paths so runs cannot mix or overwrite earlier captures.
		if err := os.Mkdir(*captureDir, 0700); err != nil {
			log.Fatal(err)
		}
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := &http.Server{
		Addr:              *listen,
		Handler:           newProxy(target, apiKey, *captureDir, logger),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: SSE/WS connections can remain open for a full turn.
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "Listening on http://%s -> %s; authorization override=%t\n", listener.Addr(), target, apiKey != "")
	log.Fatal(server.Serve(listener))
}
