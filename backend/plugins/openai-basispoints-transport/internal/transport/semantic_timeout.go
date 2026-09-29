package transport

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"time"
)

// semanticTimeoutError distinguishes an SSE event idle timeout from a normal
// connection failure while still satisfying net.Error for stable diagnostics.
type semanticTimeoutError struct{}

func (semanticTimeoutError) Error() string   { return "SSE 语义事件空闲超时" }
func (semanticTimeoutError) Timeout() bool   { return true }
func (semanticTimeoutError) Temporary() bool { return true }

// semanticTimeoutBody enforces the proxy's own event-level idle budget. SSE
// comments are deliberately ignored: they describe transport activity and do
// not reset the timer used by Codex's eventsource reader.
type semanticTimeoutBody struct {
	body        io.ReadCloser
	sse         bool
	duration    time.Duration
	timer       *time.Timer
	done        chan struct{}
	expired     chan struct{}
	doneOnce    sync.Once
	expiredOnce sync.Once
	mu          sync.Mutex
	line        []byte
	data        bool
}

func newSemanticTimeoutBody(body io.ReadCloser, timeout time.Duration, sse bool) *semanticTimeoutBody {
	r := &semanticTimeoutBody{body: body, sse: sse, duration: timeout, timer: time.NewTimer(timeout), done: make(chan struct{}), expired: make(chan struct{})}
	go r.watch()
	return r
}

func (r *semanticTimeoutBody) watch() {
	select {
	case <-r.timer.C:
		r.expiredOnce.Do(func() { close(r.expired) })
		_ = r.body.Close()
	case <-r.done:
	}
}

func (r *semanticTimeoutBody) touch() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.timer.Stop() {
		select {
		case <-r.timer.C:
		default:
		}
	}
	r.timer.Reset(r.duration)
}

func (r *semanticTimeoutBody) Read(p []byte) (int, error) {
	select {
	case <-r.expired:
		return 0, semanticTimeoutError{}
	default:
	}
	n, err := r.body.Read(p)
	if r.sse && n > 0 {
		r.observe(p[:n])
	}
	select {
	case <-r.expired:
		if n == 0 {
			return 0, semanticTimeoutError{}
		}
	default:
	}
	return n, err
}

func (r *semanticTimeoutBody) observe(chunk []byte) {
	for len(chunk) > 0 {
		n := bytes.IndexByte(chunk, '\n')
		if n < 0 {
			r.line = append(r.line, chunk...)
			return
		}
		r.line = append(r.line, chunk[:n]...)
		line := strings.TrimSuffix(string(r.line), "\r")
		r.line = r.line[:0]
		if line == "" {
			if r.data {
				r.touch()
			}
			r.data = false
		} else if strings.HasPrefix(line, "data:") {
			r.data = true
		}
		chunk = chunk[n+1:]
	}
}

func (r *semanticTimeoutBody) Close() error {
	r.doneOnce.Do(func() { close(r.done) })
	r.timer.Stop()
	return r.body.Close()
}
