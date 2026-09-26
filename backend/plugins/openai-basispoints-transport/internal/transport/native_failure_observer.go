package transport

import (
	"bytes"
	"encoding/json"
)

// Observe native Responses failures without rewriting, delaying or retaining
// the forwarded response. Oversized data events are skipped independently so
// a large text/image event cannot hide a later terminal failure.
type nativeFailureObserver struct {
	sse                 bool
	line, data          []byte
	skipLine, skipEvent bool
	failed              bool
}

func (o *nativeFailureObserver) feed(chunk []byte) {
	if o.failed {
		return
	}
	if !o.sse {
		if !o.skipEvent && len(o.data)+len(chunk) <= maxErrorProbeBytes {
			o.data = append(o.data, chunk...)
		} else {
			o.data = nil
			o.skipEvent = true
		}
		return
	}
	for len(chunk) > 0 {
		n := bytes.IndexByte(chunk, '\n')
		if n < 0 {
			n = len(chunk)
		}
		if !o.skipLine && len(o.line)+n <= maxErrorProbeBytes {
			o.line = append(o.line, chunk[:n]...)
		} else {
			o.line = nil
			o.skipLine = true
		}
		if n == len(chunk) {
			return
		}
		chunk = chunk[n+1:]
		o.finishLine()
	}
}

func (o *nativeFailureObserver) finishLine() {
	line := bytes.TrimSuffix(o.line, []byte{'\r'})
	if o.skipLine {
		o.skipEvent = true
	} else if len(line) == 0 {
		if !o.skipEvent {
			o.inspect(o.data)
		}
		o.data, o.skipEvent = nil, false
	} else if bytes.HasPrefix(line, []byte("event:")) {
		switch string(bytes.TrimSpace(line[6:])) {
		case "error", "response.failed", "response.incomplete":
			o.failed = true
		}
	} else if bytes.HasPrefix(line, []byte("data:")) && !o.skipEvent {
		part := bytes.TrimPrefix(line[5:], []byte{' '})
		if len(o.data)+len(part)+1 > maxErrorProbeBytes {
			o.data = nil
			o.skipEvent = true
		} else {
			o.data = append(o.data, part...)
			o.data = append(o.data, '\n')
		}
	}
	o.line, o.skipLine = nil, false
}

func (o *nativeFailureObserver) finish() bool {
	if o.sse && (len(o.line) > 0 || o.skipLine) {
		o.finishLine()
	}
	if !o.skipEvent {
		o.inspect(o.data)
	}
	return o.failed
}

func (o *nativeFailureObserver) inspect(raw []byte) {
	var value struct {
		Type, Status string
		Error        json.RawMessage
		Response     struct{ Status string }
	}
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	o.failed = o.failed || value.Type == "error" || value.Type == "response.failed" || value.Type == "response.incomplete" ||
		value.Status == "failed" || value.Status == "incomplete" || value.Response.Status == "failed" || value.Response.Status == "incomplete" ||
		(len(value.Error) > 0 && !bytes.Equal(value.Error, []byte("null")))
}
