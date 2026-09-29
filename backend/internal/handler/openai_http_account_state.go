package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/responsesstate"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Observe the final HTTP wire, so native, passthrough, compact and plugin output
// all acquire provenance. No tool arguments or ciphertext are stored here.
// Keepalives may write concurrently; the writer serializes observation/writes.
type httpAccountStateWriter struct {
	gin.ResponseWriter
	mu            sync.Mutex
	accountID     int64
	pending, data []byte
	seen          map[string]bool
	failure       error
	disabled      bool
	record        func(int64, []string) error
}

func (w *httpAccountStateWriter) selectAccount(id int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.accountID = id
}
func (w *httpAccountStateWriter) state() (error, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failure, len(w.seen) > 0
}
func (w *httpAccountStateWriter) disable() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.disabled = true
}
func (w *httpAccountStateWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *httpAccountStateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.disabled {
		if w.failure != nil {
			return 0, w.failure
		}
		if w.accountID > 0 && w.Status() < 400 {
			if err := w.observe(p); err != nil {
				w.failure = err
				return 0, err
			}
		}
	}
	return w.ResponseWriter.Write(p)
}
func (w *httpAccountStateWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ResponseWriter.Flush()
}
func (w *httpAccountStateWriter) observe(p []byte) error {
	const limit = 64 << 20
	if len(w.pending)+len(w.data)+len(p) > limit {
		return fmt.Errorf("Responses state observation exceeded %d bytes", limit)
	}
	w.pending = append(w.pending, p...)
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		if !json.Valid(w.pending) {
			return nil
		}
		err := w.observeJSON(w.pending)
		w.pending = nil
		return err
	}
	for {
		n := bytes.IndexByte(w.pending, '\n')
		if n < 0 {
			return nil
		}
		line := bytes.TrimSuffix(w.pending[:n], []byte{'\r'})
		if len(line) == 0 {
			if err := w.observeJSON(w.data); err != nil {
				return err
			}
			w.data = nil
		} else if bytes.HasPrefix(line, []byte("data:")) {
			part := bytes.TrimPrefix(line[5:], []byte(" "))
			if len(w.data) > 0 {
				w.data = append(w.data, '\n')
			}
			w.data = append(w.data, part...)
		}
		w.pending = w.pending[n+1:]
	}
}
func (w *httpAccountStateWriter) observeJSON(raw []byte) error {
	keys := responsesstate.OutputKeys(raw)
	fresh := make([]string, 0, len(keys))
	for _, key := range keys {
		if !w.seen[key] {
			fresh = append(fresh, key)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	if err := w.record(w.accountID, fresh); err != nil {
		return err
	}
	if w.seen == nil {
		w.seen = make(map[string]bool)
	}
	for _, key := range fresh {
		w.seen[key] = true
	}
	return nil
}

func (h *OpenAIGatewayHandler) writeHTTPAccountStateError(c *gin.Context, err error) {
	status, code, message := http.StatusServiceUnavailable, "account_state_store_unavailable", "Account-bound history ownership is temporarily unavailable; retry later."
	var stateErr *service.OpenAIHTTPAccountStateError
	if errors.As(err, &stateErr) {
		status, code, message = stateErr.Status, stateErr.Code, stateErr.Message
	}
	service.StopOpenAICompactSSEKeepaliveCommitted(c)
	if !c.Writer.Written() {
		c.Header("Content-Type", "application/json")
		c.Header("Content-Length", "")
		c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "code": code, "message": message}})
		return
	}
	// Codex maps invalid_prompt to a terminal InvalidPrompt error; an unknown
	// response.failed code is a retryable Stream error. Keep our diagnosis separate.
	wireCode := "invalid_prompt"
	if status >= 500 {
		wireCode = "server_error"
	}
	frame, _ := json.Marshal(gin.H{"type": "response.failed", "response": gin.H{"status": "failed", "output": []any{}, "error": gin.H{"type": "invalid_request_error", "code": wireCode, "reason": code, "message": message}}})
	_, _ = fmt.Fprintf(c.Writer, "event: response.failed\ndata: %s\n\n", frame)
	c.Writer.Flush()
}
