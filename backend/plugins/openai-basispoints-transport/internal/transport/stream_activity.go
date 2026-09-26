package transport

import (
	"io"
	"time"
)

const upstreamActivityInterval = 5 * time.Second
const upstreamActivityComment = ": bps-upstream-activity\n\n"

// Tool batches and feedback responses are buffered before validation. Report
// actual upstream reads as SSE comments so buffering is not mistaken for an
// idle connection by the host. No timer generates activity on a stalled read.
// This runs on the same goroutine as the bridge, between complete SSE writes.
type streamActivity struct {
	emit func([]byte) error
	now  func() time.Time
	last time.Time
}

func (a *streamActivity) wrap(body io.ReadCloser) io.ReadCloser {
	return &activityReadCloser{ReadCloser: body, activity: a}
}

type activityReadCloser struct {
	io.ReadCloser
	activity *streamActivity
}

func (r *activityReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		a := r.activity
		now := a.now()
		if a.last.IsZero() || now.Sub(a.last) >= upstreamActivityInterval {
			if sendErr := a.emit([]byte(upstreamActivityComment)); sendErr != nil {
				return 0, sendErr
			}
			a.last = now
		}
	}
	return n, err
}
