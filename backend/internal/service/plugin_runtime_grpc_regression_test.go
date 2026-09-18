package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// No external signed package or provider is needed. Both ends use the selected
// grpc dependency; the normal subprocess handshake and host runtime are real.
func TestPluginRuntimeGRPCSubprocess(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "transport")
	build := exec.Command("go", "build", "-p", "2", "-o", binary, "./testdata/grpc_transport")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)
	data, err := os.ReadFile(binary)
	require.NoError(t, err)
	digest := sha256.Sum256(data)
	socketDir := filepath.Join(root, "runtime")
	require.NoError(t, os.Mkdir(socketDir, 0700))
	runtime, err := startPluginRuntime(context.Background(), &PluginInstallation{
		PluginKey: "test.grpc.transport", Version: "1.0.0", BinaryPath: binary, BinarySHA256: hex.EncodeToString(digest[:]),
	}, 10*time.Second, socketDir)
	require.NoError(t, err)
	t.Cleanup(runtime.kill)
	require.NoError(t, runtime.checkHealth(context.Background()))
	normalized, err := runtime.validateAndApplyNormalizedConfig(context.Background(), []byte(`{"enabled":true}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":true}`, string(normalized))

	var calls atomic.Int64
	entered := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/cancel" {
			entered <- struct{}{}
			<-r.Context().Done()
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("X-Echo", r.Header.Get("X-Test"))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	}))
	t.Cleanup(upstream.Close)
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1}
	payload := bytes.Repeat([]byte("grpc-body-"), 32768)
	req, err := http.NewRequest(http.MethodPost, upstream.URL, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("X-Test", "streamed")
	require.True(t, runtime.beginRequest())
	resp, err := runtime.roundTrip(context.Background(), req, "", account)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "streamed", resp.Header.Get("X-Echo"))
	require.Equal(t, payload, body)
	require.Zero(t, runtime.inFlight.Load())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL+"/cancel", nil)
	require.NoError(t, err)
	require.True(t, runtime.beginRequest())
	result := make(chan error, 1)
	go func() {
		_, err := runtime.roundTrip(ctx, req, "", account)
		runtime.finishRequest()
		result <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("plugin did not reach the HTTP mock")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled plugin stream did not finish")
	}
	require.Zero(t, runtime.inFlight.Load())
	require.EqualValues(t, 2, calls.Load(), "cancelled work must not be replayed")
}
