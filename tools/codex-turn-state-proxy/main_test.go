package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func startTestProxy(t *testing.T, upstream, apiKey, captureDir string) (*httptest.Server, *logBuffer) {
	t.Helper()
	target, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuffer{}
	server := httptest.NewServer(newProxy(target, apiKey, captureDir, slog.New(slog.NewJSONHandler(logs, nil))))
	t.Cleanup(server.Close)
	return server, logs
}

func TestProxyFlushesSSEBeforeUpstreamCompletes(t *testing.T) {
	finish := make(chan struct{})
	defer close(finish)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(turnStateHeader); got != "inbound-state" {
			t.Errorf("upstream inbound turn state = %q", got)
		}
		if r.URL.RequestURI() != "/v1/responses?test=1" || r.Header.Get("Authorization") != "Bearer client-secret" {
			t.Error("path, query, or authorization changed")
		}
		w.Header().Set(turnStateHeader, "upstream-state")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: ok\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-finish:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(upstream.Close)
	server, logs := startTestProxy(t, upstream.URL, "", "")

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses?test=1", strings.NewReader(`{"model":"test","input":"private-body"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(turnStateHeader, "inbound-state")
	req.Header.Set("Authorization", "Bearer client-secret")
	req.Header.Set("Cookie", "private-cookie")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get(turnStateHeader); got != "upstream-state" {
		t.Fatalf("downstream turn state = %q", got)
	}
	// The upstream is still waiting: a buffering proxy would time out here.
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "data: ok\n" {
		t.Fatalf("downstream SSE line = %q", line)
	}
	if !strings.Contains(logs.String(), "inbound-state") || !strings.Contains(logs.String(), "upstream-state") {
		t.Fatalf("turn state values missing from logs: %s", logs.String())
	}
	for _, private := range []string{"client-secret", "private-cookie", "private-body"} {
		if strings.Contains(logs.String(), private) {
			t.Errorf("log contains %s", private)
		}
	}
}

func TestProxyPreservesMissingOrEmptyHeadersOnErrorResponses(t *testing.T) {
	for _, emptyHeader := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "empty"}[emptyHeader], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer upstream-secret" {
					t.Error("authorization override not applied")
				}
				if len(r.Header.Values(turnStateHeader)) != 0 {
					t.Error("unexpected injected turn state")
				}
				if emptyHeader {
					w.Header().Set(turnStateHeader, "")
				}
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"error":"busy"}`)
			}))
			t.Cleanup(upstream.Close)
			server, logs := startTestProxy(t, upstream.URL, "upstream-secret", "")
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Get(server.URL + "/v1/responses")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != 429 || string(body) != `{"error":"busy"}` {
				t.Fatalf("error response changed: status=%d body=%q err=%v", resp.StatusCode, body, err)
			}
			if (len(resp.Header.Values(turnStateHeader)) > 0) != emptyHeader {
				t.Fatal("empty and absent header not distinguished")
			}
			if !strings.Contains(logs.String(), `"status":429`) || strings.Contains(logs.String(), "upstream-secret") {
				t.Fatalf("incorrect response log: %s", logs.String())
			}
			want := `"present":false,"turn_state":null`
			if emptyHeader {
				want = `"present":true,"turn_state":[""]`
			}
			if !strings.Contains(logs.String(), want) {
				t.Fatalf("missing header presence record: %s", logs.String())
			}
		})
	}
}

func TestProxyRelaysWebSocketHandshakeAndFrames(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(turnStateHeader) != "ws-inbound" || r.Header.Get("Upgrade") != "websocket" {
			t.Error("websocket handshake headers changed")
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\nX-Codex-Turn-State: ws-upstream\r\n\r\n")
		_ = rw.Flush()
		// A masked client text frame containing "hi".
		frame := make([]byte, 8)
		if _, err := io.ReadFull(rw, frame); err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(frame, []byte{0x81, 0x82, 1, 2, 3, 4, 'h' ^ 1, 'i' ^ 2}) {
			t.Errorf("client frame changed: %x", frame)
		}
		_, _ = rw.Write([]byte{0x81, 2, 'h', 'i'})
		_ = rw.Flush()
	}))
	t.Cleanup(upstream.Close)
	server, logs := startTestProxy(t, upstream.URL, "", t.TempDir())
	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = io.WriteString(conn, "GET /v1/responses HTTP/1.1\r\nHost: localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nX-Codex-Turn-State: ws-inbound\r\n\r\n")
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 101 || resp.Header.Get(turnStateHeader) != "ws-upstream" {
		t.Fatal("upgrade response or turn state lost")
	}
	_, _ = conn.Write([]byte{0x81, 0x82, 1, 2, 3, 4, 'h' ^ 1, 'i' ^ 2})
	frame := make([]byte, 4)
	if _, err := io.ReadFull(reader, frame); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame, []byte{0x81, 2, 'h', 'i'}) {
		t.Fatalf("server frame changed: %x", frame)
	}
	if !strings.Contains(logs.String(), `"status":101`) || !strings.Contains(logs.String(), "ws-upstream") {
		t.Fatal("missing websocket response capture")
	}
}

func TestFullCapturePreservesStreamingMetadataAndRedactsCredentials(t *testing.T) {
	dir := t.TempDir()
	finish := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(finish) }) }
	defer release()
	requestBody := `{"model":"test-model","input":"hello","stream":true}`
	first := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\",\"model\":\"reported-model\"}}\n\n"
	last := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"status\":\"completed\",\"model\":\"reported-model\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"output_tokens_details\":{\"reasoning_tokens\":1},\"total_tokens\":12}}}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != requestBody || r.Header.Get("Authorization") != "Bearer real-upstream-key" || r.Header.Get("Cookie") != "client-cookie" {
			t.Error("upstream request differs from intended request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Set-Cookie", "upstream-cookie")
		w.Header().Set("Connection", "X-Upstream-Hop")
		w.Header().Set("X-Upstream-Hop", "hop-marker")
		w.Header().Set(turnStateHeader, "full-capture-state")
		w.Header().Set("Trailer", "X-End-Marker")
		_, _ = io.WriteString(w, first)
		w.(http.Flusher).Flush()
		select {
		case <-finish:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, last)
		w.Header().Set("X-End-Marker", "done")
	}))
	t.Cleanup(upstream.Close)
	server, _ := startTestProxy(t, upstream.URL, "real-upstream-key", dir)
	req, _ := http.NewRequest("POST", server.URL+"/v1/responses", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer client-placeholder")
	req.Header.Set("Cookie", "client-cookie")
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, len(first))
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != first {
		t.Fatalf("capture buffered the stream: %v", err)
	}
	release()
	rest, err := io.ReadAll(resp.Body)
	if err != nil || string(rest) != last {
		t.Fatalf("response changed: %q, %v", rest, err)
	}
	if resp.Header.Get("Set-Cookie") != "upstream-cookie" || resp.Header.Get("X-Upstream-Hop") != "" {
		t.Fatal("redaction changed forwarding or hop header was not stripped")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("capture directories = %d", len(entries))
	}
	runDir := filepath.Join(dir, entries[0].Name())
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if string(read("request.body")) != requestBody || string(read("response.body")) != first+last {
		t.Fatal("captured body differs from actual body")
	}
	responseMeta := string(read("response.json"))
	if !strings.Contains(responseMeta, "hop-marker") || !strings.Contains(responseMeta, "full-capture-state") {
		t.Fatal("upstream headers were captured after filtering")
	}
	for _, name := range []string{"request.json", "request.wire-headers.jsonl", "response.json", "response.end.json"} {
		data := string(read(name))
		for _, private := range []string{"real-upstream-key", "client-placeholder", "client-cookie", "upstream-cookie"} {
			if strings.Contains(data, private) {
				t.Fatalf("credential leaked to %s", name)
			}
		}
		info, _ := os.Stat(filepath.Join(runDir, name))
		if info.Mode().Perm() != 0600 {
			t.Fatalf("capture permissions: %s", info.Mode())
		}
	}
	wireHeaders := string(read("request.wire-headers.jsonl"))
	if !strings.Contains(wireHeaders, "Content-Length") || !strings.Contains(wireHeaders, "Host") {
		t.Fatal("transport-generated headers not captured")
	}
	var end struct {
		Complete bool        `json:"complete"`
		Bytes    int         `json:"body_bytes"`
		Trailers http.Header `json:"trailers"`
	}
	if err := json.Unmarshal(read("response.end.json"), &end); err != nil {
		t.Fatal(err)
	}
	if !end.Complete || end.Bytes != len(first+last) || end.Trailers.Get("X-End-Marker") != "done" {
		t.Fatalf("incomplete capture: %+v", end)
	}
}
