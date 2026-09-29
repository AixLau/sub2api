package transport

import (
	"encoding/json"
	"errors"
	"net/http"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/bridge"
	"google.golang.org/grpc"
)

// Called before response headers. Semantic failures end normally at the RPC
// transport layer so they cannot masquerade as a network disconnect.
func (p *Plugin) sendSemanticFailure(stream grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse], body []byte, code string, cause error, snapshot json.RawMessage) error {
	diagnosticsFrom(stream.Context()).fail(code)
	p.lastBridgeError.Store(code)
	response := bridge.FailureResponse(code, cause, snapshot)
	status, contentType := http.StatusBadRequest, "application/json"
	if errors.Is(cause, bridge.ErrStateStoreUnavailable) {
		status = http.StatusServiceUnavailable
	}
	// stream:true is a requested success format, not permission to turn a
	// deterministic pre-header rejection into HTTP 200 plus a retryable event.
	var failed map[string]json.RawMessage
	_ = json.Unmarshal(response, &failed)
	data, _ := json.Marshal(map[string]json.RawMessage{"error": failed["error"]})
	p.stats.lastCode.Store(int64(status))
	start := &http.Response{StatusCode: status, Status: http.StatusText(status), Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Content-Type": []string{contentType}}, ContentLength: int64(len(data))}
	if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: responseStart(start)}}); err != nil {
		return err
	}
	if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: data}}); err != nil {
		return err
	}
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{BytesReceived: int64(len(data))}}})
}
