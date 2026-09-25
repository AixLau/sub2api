package bridge

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type terminalThenErrorReader struct {
	data []byte
	read bool
}

func (r *terminalThenErrorReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		n := copy(p, r.data)
		return n, nil
	}
	return 0, errors.New("connection reset by peer")
}

func TestStreamStopsReadingAfterTerminalEvent(t *testing.T) {
	r := prepareWeather(t, memoryStore{})
	input := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-terminal\",\"status\":\"completed\",\"output\":[]}}\n\n"
	var frames strings.Builder
	err := r.Stream(context.Background(), &terminalThenErrorReader{data: []byte(input)}, func(b []byte) error {
		frames.Write(b)
		return nil
	})
	require.NoError(t, err)
	require.Contains(t, frames.String(), "response.completed")
}

func TestStreamPreservesReadErrorBeforeTerminal(t *testing.T) {
	r := prepareWeather(t, memoryStore{})
	reader := io.MultiReader(strings.NewReader("data: {\"type\":\"response.created\"}\n\n"), errReader{})
	err := r.Stream(context.Background(), reader, func([]byte) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "读取上游 SSE 失败")
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset by peer") }
