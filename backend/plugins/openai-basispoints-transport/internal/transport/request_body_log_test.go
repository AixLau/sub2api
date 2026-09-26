package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type bodyLogPart struct {
	Message   string `json:"@message"`
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
	Body      string `json:"request_body"`
	SHA256    string `json:"request_body_sha256"`
	Encoding  string `json:"request_body_encoding"`
	Complete  bool   `json:"request_body_complete"`
	Bytes     int    `json:"request_body_bytes"`
	Part      int    `json:"part"`
	Parts     int    `json:"parts"`
}

func loggedBodyParts(t *testing.T, logs string) []bodyLogPart {
	t.Helper()
	var parts []bodyLogPart
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if line == "" {
			continue
		}
		var part bodyLogPart
		require.NoError(t, json.Unmarshal([]byte(line), &part))
		if part.Message != "bps.failed_request_body" {
			continue
		}
		// go-plugin cannot preserve stderr lines beyond 64 KiB.
		require.Less(t, len(line), 64<<10)
		parts = append(parts, part)
	}
	return parts
}

func assertLoggedBody(t *testing.T, parts []bodyLogPart, body []byte, complete bool) {
	t.Helper()
	require.NotEmpty(t, parts)
	sum := sha256.Sum256(body)
	var restored bytes.Buffer
	for i, part := range parts {
		require.Equal(t, i+1, part.Part)
		require.Equal(t, len(parts), part.Parts)
		require.Equal(t, len(body), part.Bytes)
		require.Equal(t, complete, part.Complete)
		require.Equal(t, hex.EncodeToString(sum[:]), part.SHA256)
		require.Equal(t, parts[0].TraceID, part.TraceID)
		require.Equal(t, parts[0].RequestID, part.RequestID)
		decoded := []byte(part.Body)
		if part.Encoding == "base64" {
			var err error
			decoded, err = base64.StdEncoding.DecodeString(part.Body)
			require.NoError(t, err)
		} else {
			require.Equal(t, "utf-8", part.Encoding)
		}
		restored.Write(decoded)
	}
	require.Equal(t, string(body), restored.String())
}

func TestFailedRequestBodyExactBytes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     []byte
		complete bool
	}{
		{"empty", []byte{}, true},
		{"precision_and_whitespace", []byte(" \n" + `{"id":9007199254740993,"fraction":0.1234567890123456789,"code":"unmodified"}` + " \n"), true},
		{"multibyte_boundaries", []byte(strings.Repeat("中😀文\n", 10000)), true},
		{"worst_case_escaping", bytes.Repeat([]byte{0, 1, '\t', '\n', '"', '\r'}, 12000), true},
		{"invalid_utf8", bytes.Repeat([]byte{0xff, 0xfe, 0, 'a'}, 5000), true},
		{"partial", []byte(`{"input":"unfinished`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs lockedLogBuffer
			p := New()
			p.diagnosticLogger = hclog.New(&hclog.LoggerOptions{JSONFormat: true, Output: &logs, Level: hclog.Info})
			_, d := p.startDiagnostics(context.Background(), &pluginv1.ForwardRequestStart{RequestId: "body_test"})
			d.body(tc.body, tc.complete)
			d.fail("TEST_FAILURE")
			d.fail("ANOTHER_FAILURE")
			d.finish(nil)
			parts := loggedBodyParts(t, logs.text())
			assertLoggedBody(t, parts, tc.body, tc.complete)
			entries := p.recentDiagnostics.snapshot()
			require.Len(t, entries, 1)
			snapshot := entries[0].RequestBody
			require.True(t, snapshot.Available)
			require.Equal(t, tc.complete, snapshot.Complete)
			require.Equal(t, parts[0].SHA256, snapshot.SHA256)
			require.Equal(t, parts[0].Body, snapshot.Preview)
			require.Equal(t, len(parts) > 1, snapshot.PreviewTruncated)
			require.Equal(t, len(parts), snapshot.Parts)
		})
	}
}

func TestFailedRequestBodyUnavailable(t *testing.T) {
	var logs lockedLogBuffer
	p := New()
	p.diagnosticLogger = hclog.New(&hclog.LoggerOptions{JSONFormat: true, Output: &logs, Level: hclog.Info})
	_, d := p.startDiagnostics(context.Background(), &pluginv1.ForwardRequestStart{})
	d.fail("PLUGIN_INVALID_REQUEST")
	require.Empty(t, loggedBodyParts(t, logs.text()))
	require.False(t, p.recentDiagnostics.snapshot()[0].RequestBody.Available)
}

type bodyInputTestStream struct {
	grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]
	frames []*pluginv1.ForwardRequest
}

func (s *bodyInputTestStream) Recv() (*pluginv1.ForwardRequest, error) {
	if len(s.frames) == 0 {
		return nil, io.EOF
	}
	frame := s.frames[0]
	s.frames = s.frames[1:]
	return frame, nil
}

func TestReadRequestBodyRetainsIncompletePrefix(t *testing.T) {
	prefix := []byte(`{"input":"unfinished`)
	for _, tc := range []struct {
		name string
		tail *pluginv1.ForwardRequest
	}{
		{"interrupted", nil},
		{"length_mismatch", &pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyEnd{BodyEnd: true}}},
		{"invalid_order", &pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_Start{Start: &pluginv1.ForwardRequestStart{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &bodyInputTestStream{frames: []*pluginv1.ForwardRequest{{Frame: &pluginv1.ForwardRequest_BodyChunk{BodyChunk: prefix}}}}
			if tc.tail != nil {
				stream.frames = append(stream.frames, tc.tail)
			}
			body, err := readRequestBody(stream, &pluginv1.ForwardRequestStart{HasBody: true, ContentLength: int64(len(prefix) + 1)})
			require.Error(t, err)
			require.Equal(t, prefix, body)
		})
	}
}

func TestFailedRequestBodiesConcurrent(t *testing.T) {
	var logs lockedLogBuffer
	p := New()
	p.diagnosticLogger = hclog.New(&hclog.LoggerOptions{JSONFormat: true, Output: &logs, Level: hclog.Info})
	var wg sync.WaitGroup
	want := map[string][]byte{}
	for n := 0; n < 12; n++ {
		id := fmt.Sprintf("req_%d", n)
		body := []byte(strings.Repeat(id, 3000))
		want[id] = body
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, d := p.startDiagnostics(context.Background(), &pluginv1.ForwardRequestStart{RequestId: id})
			d.body(body, true)
			d.fail("TEST_FAILURE")
		}()
	}
	wg.Wait()
	groups := map[string][]bodyLogPart{}
	traces := map[string]bool{}
	for _, part := range loggedBodyParts(t, logs.text()) {
		groups[part.RequestID] = append(groups[part.RequestID], part)
	}
	require.Len(t, groups, len(want))
	for id, parts := range groups {
		assertLoggedBody(t, parts, want[id], true)
		require.False(t, traces[parts[0].TraceID])
		traces[parts[0].TraceID] = true
	}
}
