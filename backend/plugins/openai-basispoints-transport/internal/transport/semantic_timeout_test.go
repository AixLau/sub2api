package transport

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSemanticTimeoutIgnoresCommentsAndResetsOnDataEvents(t *testing.T) {
	body := newSemanticTimeoutBody(io.NopCloser(strings.NewReader(": keepalive\n\ndata: {\"type\":\"response.completed\"}\n\n")), 20*time.Millisecond, true)
	defer body.Close()
	buf := make([]byte, 256)
	n, err := body.Read(buf)
	require.NoError(t, err)
	require.Positive(t, n)
	// The complete read contains a semantic event, so it is still live after
	// the initial deadline and the comment alone did not count as progress.
	time.Sleep(5 * time.Millisecond)
	_, err = body.Read(buf)
	require.ErrorIs(t, err, io.EOF)
}

func TestSemanticTimeoutExpiresWhenOnlyCommentsArrive(t *testing.T) {
	body := newSemanticTimeoutBody(&commentThenStallReader{}, 10*time.Millisecond, true)
	defer body.Close()
	buf := make([]byte, 64)
	_, err := body.Read(buf)
	require.NoError(t, err)
	_, err = body.Read(buf)
	require.ErrorIs(t, err, semanticTimeoutError{})
}

type commentThenStallReader struct{ first bool }

func (r *commentThenStallReader) Read(p []byte) (int, error) {
	if !r.first {
		r.first = true
		return copy(p, ": keepalive\n\n"), nil
	}
	time.Sleep(100 * time.Millisecond)
	return 0, io.EOF
}

func (r *commentThenStallReader) Close() error { return nil }
