package plugin

import (
	"net/http"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
)

// streamWriter is an http.ResponseWriter that turns the bps-local handler's
// output into Forward response frames: one start frame, then body chunks.
// It is used from a single goroutine (the handler), matching gRPC Send rules.
type streamWriter struct {
	stream forwardStream
	header http.Header

	started bool
	status  int
	bytes   int64
	err     error

	proto                  string
	protoMajor, protoMinor int
	contentLength          int64
}

func newStreamWriter(stream forwardStream) *streamWriter {
	return &streamWriter{
		stream: stream, header: make(http.Header),
		proto: "HTTP/1.1", protoMajor: 1, protoMinor: 1, contentLength: -1,
	}
}

func (w *streamWriter) Header() http.Header { return w.header }

func (w *streamWriter) WriteHeader(status int) {
	if w.started || w.err != nil {
		return
	}
	if status < 100 || status > 599 {
		status = http.StatusBadGateway
	}
	w.started = true
	w.status = status
	w.err = w.stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{
		StatusCode:    int32(status),
		Status:        http.StatusText(status),
		Protocol:      w.proto,
		ProtocolMajor: int32(w.protoMajor),
		ProtocolMinor: int32(w.protoMinor),
		Headers:       headersToProto(w.header),
		ContentLength: w.contentLength,
	}}})
}

func (w *streamWriter) Write(data []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	if w.err != nil {
		return 0, w.err
	}
	written := 0
	for len(data) > 0 {
		n := len(data)
		if n > chunkSize {
			n = chunkSize
		}
		chunk := append([]byte(nil), data[:n]...)
		if err := w.stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: chunk}}); err != nil {
			w.err = err
			return written, err
		}
		written += n
		w.bytes += int64(n)
		data = data[n:]
	}
	return written, nil
}

// Flush satisfies http.Flusher; every Write is already a sent frame.
func (w *streamWriter) Flush() {}
