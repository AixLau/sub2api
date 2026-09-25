package transport

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const maxErrorProbeBytes = 256 << 10

type replayBody struct {
	io.Reader
	io.Closer
}

// probeUpstreamFailure inspects only the pre-output SSE prefix. All consumed
// bytes are restored verbatim; any semantic event irrevocably ends the probe.
// This permits one safe invalid-encryption retry before publishing output.
func probeUpstreamFailure(response *http.Response) (code, message string, err error) {
	original := response.Body
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, readErr := io.ReadAll(io.LimitReader(original, maxErrorProbeBytes+1))
		response.Body = &replayBody{Reader: io.MultiReader(bytes.NewReader(raw), original), Closer: original}
		if readErr != nil {
			return "", "", readErr
		}
		if len(raw) > maxErrorProbeBytes {
			return "", "", nil
		}
		code, message = responseFailure(raw)
		return code, message, nil
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		return "", "", nil
	}
	reader := bufio.NewReaderSize(original, 16<<10)
	var prefix bytes.Buffer
	var data []string
	var partial []byte
	defer func() {
		response.Body = &replayBody{Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), reader), Closer: original}
	}()
	for events := 0; events < 32 && prefix.Len() < maxErrorProbeBytes; {
		line, readErr := reader.ReadSlice('\n')
		prefix.Write(line)
		if prefix.Len() >= maxErrorProbeBytes {
			return "", "", nil
		}
		if errors.Is(readErr, bufio.ErrBufferFull) {
			partial = append(partial, line...)
			continue
		}
		if len(partial) > 0 {
			line = append(partial, line...)
			partial = nil
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", "", readErr
		}
		text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if strings.HasPrefix(text, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(text, "data:"), " "))
		}
		if text == "" || errors.Is(readErr, io.EOF) {
			events++
			if len(data) > 0 {
				raw := []byte(strings.Join(data, "\n"))
				data = nil
				var event struct {
					Type     string
					Response struct{ Output []json.RawMessage }
				}
				if json.Unmarshal(raw, &event) != nil {
					return "", "", nil
				}
				switch event.Type {
				case "error", "response.failed":
					code, message = responseFailure(raw)
					return code, message, nil
				case "response.created", "response.in_progress":
					if len(event.Response.Output) > 0 {
						return "", "", nil
					}
				default:
					return "", "", nil
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return "", "", nil
		}
	}
	return "", "", nil
}

func responseFailure(raw []byte) (string, string) {
	type failure struct {
		Code    string
		Message string
	}
	var value struct {
		Code     string
		Message  string
		Error    failure
		Response struct{ Error failure }
	}
	if json.Unmarshal(raw, &value) != nil {
		return "", ""
	}
	if value.Response.Error.Code != "" {
		return value.Response.Error.Code, value.Response.Error.Message
	}
	if value.Error.Code != "" {
		return value.Error.Code, value.Error.Message
	}
	return value.Code, value.Message
}
