// Command codex-turn-state-proxy observes Codex turn-state headers while
// forwarding HTTP, SSE, and WebSocket traffic to a fixed upstream.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const turnStateHeader = "X-Codex-Turn-State"

type requestIDKey struct{}

func newProxy(target *url.URL, apiKey string, logger *slog.Logger) http.Handler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Preserve the client's encoding choice; don't add/decode gzip implicitly.
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = 2 * time.Minute

	proxy := &httputil.ReverseProxy{
		Transport:     transport,
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

func main() {
	listen := flag.String("listen", "127.0.0.1:18080", "loopback listen address")
	upstream := flag.String("upstream", "", "upstream base URL, e.g. https://api.example.com (without /v1 if the client sends /v1)")
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := &http.Server{
		Addr:              *listen,
		Handler:           newProxy(target, apiKey, logger),
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
