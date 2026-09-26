package transport

import (
	"encoding/json"
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
	var request struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &request)
	response := bridge.FailureResponse(code, cause, snapshot)
	status, contentType := http.StatusBadRequest, "application/json"
	var data []byte
	if request.Stream {
		status, contentType = http.StatusOK, "text/event-stream"
		frame, _ := json.Marshal(map[string]any{"type": "response.failed", "sequence_number": 0, "response": json.RawMessage(response)})
		data = append([]byte("event: response.failed\ndata: "), frame...)
		data = append(data, '\n', '\n')
	} else {
		var failed map[string]json.RawMessage
		_ = json.Unmarshal(response, &failed)
		data, _ = json.Marshal(map[string]json.RawMessage{"error": failed["error"]})
	}
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
