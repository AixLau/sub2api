//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type cancelableSlowBody struct {
	once     sync.Once
	canceled chan struct{}
	closed   chan struct{}
}

func newCancelableSlowBody() *cancelableSlowBody {
	return &cancelableSlowBody{canceled: make(chan struct{}), closed: make(chan struct{})}
}

func (b *cancelableSlowBody) Read([]byte) (int, error) {
	select {
	case <-b.canceled:
		return 0, context.Canceled
	case <-b.closed:
		return 0, io.EOF
	}
}

func (b *cancelableSlowBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func (b *cancelableSlowBody) CancelRead() {
	b.once.Do(func() { close(b.canceled) })
}

func TestReadOpenAI429ErrorBodyCompletesNormally(t *testing.T) {
	svc := &OpenAIGatewayService{}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(`{"error":{"code":"rate_limit_exceeded"}}`))}

	body, complete := svc.readOpenAI429ErrorBody(context.Background(), resp)

	require.True(t, complete)
	require.Contains(t, string(body), "rate_limit_exceeded")
}

func TestReadOpenAI429ErrorBodyCancelsSlowCancelableBody(t *testing.T) {
	svc := &OpenAIGatewayService{}
	body := newCancelableSlowBody()
	resp := &http.Response{Body: body}
	started := time.Now()

	data, complete := svc.readOpenAI429ErrorBody(context.Background(), resp)

	require.False(t, complete)
	require.Empty(t, data)
	require.Less(t, time.Since(started), time.Second)
	select {
	case <-body.canceled:
	default:
		t.Fatal("429 body reader was not canceled")
	}
}

func TestReadOpenAI429ErrorBodyHonorsClientCancellation(t *testing.T) {
	svc := &OpenAIGatewayService{}
	body := newCancelableSlowBody()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	_, complete := svc.readOpenAI429ErrorBody(ctx, &http.Response{Body: body})

	require.False(t, complete)
	require.Less(t, time.Since(started), 500*time.Millisecond)
	select {
	case <-body.canceled:
	default:
		t.Fatal("client cancellation did not abort 429 body read")
	}
}

func TestReadOpenAI429ErrorBodyBoundsSize(t *testing.T) {
	svc := &OpenAIGatewayService{}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(strings.Repeat("x", int(openAI429ErrorBodyReadLimit)+1024)))}

	body, complete := svc.readOpenAI429ErrorBody(context.Background(), resp)

	require.False(t, complete)
	require.Len(t, body, int(openAI429ErrorBodyReadLimit))
}
