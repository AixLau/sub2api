package service

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const codexKeepaliveTestUA = "Codex Desktop/0.155.0-alpha.16.3 (Mac OS 26.6.2; arm64)"

func TestOpenAISSEKeepalivePayload(t *testing.T) {
	for _, tc := range []struct {
		name, userAgent, originator, want string
	}{
		{"desktop", codexKeepaliveTestUA, "", openAISSEPingEvent},
		{"originator", "", "codex_cli_rs", openAISSEPingEvent},
		{"other client", "OpenAI/Python", "", ":\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("User-Agent", tc.userAgent)
			c.Request.Header.Set("originator", tc.originator)
			require.Equal(t, tc.want, openAISSEKeepalivePayload(c, ":\n\n"))
		})
	}
	data := `{"type":"ping"}`
	require.False(t, openAIStreamDataStartsClientOutput(data, "ping"))
	require.False(t, openAIStreamDataStartsVisibleOutput(data, "ping"))
	require.False(t, openAIStreamDataStartsFirstResponse(data, "ping"))
	require.False(t, openAIStreamDataShowsStructuralProgress(data, "ping"))
}

func TestCodexCompactKeepaliveDoesNotCommitOutput(t *testing.T) {
	c, rec := newCompactBridgeTestContext(t, true)
	c.Request.Header.Set("User-Agent", codexKeepaliveTestUA)
	stop := StartOpenAICompactSSEKeepalive(c, keepaliveTestInterval)
	defer stop()
	waitForKeepaliveBeats()
	require.True(t, StopOpenAICompactSSEKeepaliveCommitted(c))
	require.Contains(t, rec.Body.String(), openAISSEPingEvent)
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
	require.True(t, writeOpenAICompactSSEBridge(c, http.StatusOK, []byte(`{"id":"resp_compact_ping","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)))
	require.Contains(t, rec.Body.String(), "response.completed")
}

// The upstream does not complete until the HTTP client receives a parsed data
// event. Comments cannot release it, even when bytes arrive continuously.
func TestCodexStreamingKeepaliveDispatchesDuringBufferedAndActiveOutput(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		passthrough, visibleOutput bool
	}{
		{"native buffered tools", false, false},
		{"native after text with frequent comments", false, true},
		{"passthrough buffered tools", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
				StreamKeepaliveInterval: 1, MaxLineSize: defaultMaxLineSize,
			}}}
			release := make(chan struct{})
			done := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				c, _ := gin.CreateTestContext(w)
				c.Request = req
				pr, pw := io.Pipe()
				defer pr.Close()
				go func() {
					defer pw.Close()
					_, _ = io.WriteString(pw, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_pending\"}}\n\n")
					if tc.visibleOutput {
						_, _ = io.WriteString(pw, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"working\"}\n\n")
					}
					comments := time.NewTicker(25 * time.Millisecond)
					defer comments.Stop()
					for {
						select {
						case <-req.Context().Done():
							return
						case <-release:
							_, _ = io.WriteString(pw, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_done\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
							return
						case <-comments.C:
							if _, err := io.WriteString(pw, ": bps-upstream-activity\n\n"); err != nil {
								return
							}
						}
					}
				}()
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: pr}
				account := &Account{ID: 1, Platform: PlatformOpenAI}
				if tc.passthrough {
					_, err := svc.handleStreamingResponsePassthrough(req.Context(), resp, c, account, time.Now(), "model", "model")
					done <- err
				} else {
					_, err := svc.handleStreamingResponse(req.Context(), resp, c, account, time.Now(), "model", "model")
					done <- err
				}
			}))
			defer server.Close()
			client := &http.Client{Timeout: 4 * time.Second}
			req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", nil)
			require.NoError(t, err)
			req.Header.Set("User-Agent", codexKeepaliveTestUA)
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			scanner := bufio.NewScanner(resp.Body)
			sawPing, sawCompleted := false, false
			for scanner.Scan() {
				line := scanner.Text()
				if line == `data: {"type":"ping"}` && !sawPing {
					sawPing = true
					close(release)
				}
				if strings.HasPrefix(line, "data: ") && strings.Contains(line, "response.completed") {
					sawCompleted = true
				}
			}
			require.NoError(t, scanner.Err())
			require.True(t, sawPing)
			require.True(t, sawCompleted)
			require.NoError(t, <-done)
		})
	}
}

func TestCodexKeepaliveDoesNotMaskUpstreamTimeoutOrPreventFailover(t *testing.T) {
	for _, firstOutput := range []bool{false, true} {
		name := "upstream inactivity"
		if firstOutput {
			name = "first output failover"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{Gateway: config.GatewayConfig{StreamKeepaliveInterval: 1, MaxLineSize: defaultMaxLineSize}}
			if firstOutput {
				cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 2
			} else {
				cfg.Gateway.StreamDataIntervalTimeout = 2
			}
			svc := &OpenAIGatewayService{cfg: cfg}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("User-Agent", codexKeepaliveTestUA)
			pr, pw := io.Pipe()
			defer pw.Close()
			defer pr.Close()
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: pr}
			result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")
			require.Error(t, err)
			require.Nil(t, result.firstTokenMs)
			require.Contains(t, rec.Body.String(), openAISSEPingEvent)
			if firstOutput {
				var failoverErr *UpstreamFailoverError
				require.ErrorAs(t, err, &failoverErr)
				require.True(t, failoverErr.SafeToFailoverAfterWrite)
				require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
			} else {
				require.Contains(t, rec.Body.String(), "stream_timeout")
			}
		})
	}
}
