package transport

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "read tcp 10.0.0.1:443: i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestUpstreamResponseReadErrorClassifiesTimeout(t *testing.T) {
	err := timeoutError{}
	require.Equal(t, "UPSTREAM_RESPONSE_TIMEOUT", upstreamResponseReadErrorCode(err, "UPSTREAM_RESPONSE_FAILED"))
	require.Equal(t, "读取上游响应超时", upstreamResponseReadErrorMessage(err))
}

func TestUpstreamResponseReadErrorClassifiesContextDeadline(t *testing.T) {
	err := context.DeadlineExceeded
	require.Equal(t, "UPSTREAM_RESPONSE_TIMEOUT", upstreamResponseReadErrorCode(err, "UPSTREAM_RESPONSE_FAILED"))
	require.Equal(t, "读取上游响应超时", upstreamResponseReadErrorMessage(err))
}

func TestUpstreamResponseReadErrorDoesNotExposeTransportDetails(t *testing.T) {
	err := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}
	require.Equal(t, "UPSTREAM_RESPONSE_FAILED", upstreamResponseReadErrorCode(err, "UPSTREAM_RESPONSE_FAILED"))
	require.Equal(t, "读取上游响应失败", upstreamResponseReadErrorMessage(err))
}
