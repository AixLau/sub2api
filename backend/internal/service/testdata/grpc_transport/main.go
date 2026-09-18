// This local test plugin exercises the public gRPC transport in a separate
// process. It only forwards to the HTTP mock supplied by the test.
package main

import (
	"bytes"
	"context"
	"io"
	"net/http"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"google.golang.org/grpc"
)

type transport struct {
	pluginv1.UnimplementedTransportPluginServer
}

func (transport) GetInfo(context.Context, *pluginv1.GetInfoRequest) (*pluginv1.GetInfoResponse, error) {
	return &pluginv1.GetInfoResponse{PluginId: "test.grpc.transport", PluginVersion: "1.0.0", ProtocolVersion: pluginv1.ProtocolVersion, TransportApiVersion: pluginv1.TransportAPIVersion}, nil
}
func (transport) Health(context.Context, *pluginv1.HealthRequest) (*pluginv1.HealthResponse, error) {
	return &pluginv1.HealthResponse{Healthy: true}, nil
}
func (transport) ValidateConfig(_ context.Context, in *pluginv1.ValidateConfigRequest) (*pluginv1.ValidateConfigResponse, error) {
	return &pluginv1.ValidateConfigResponse{Valid: true, NormalizedConfigJson: in.ConfigJson}, nil
}
func (transport) ApplyConfig(context.Context, *pluginv1.ApplyConfigRequest) (*pluginv1.ApplyConfigResponse, error) {
	return &pluginv1.ApplyConfigResponse{Applied: true}, nil
}
func (transport) Forward(stream grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	start := first.GetStart()
	var body bytes.Buffer
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		body.Write(frame.GetBodyChunk())
		if frame.GetBodyEnd() {
			break
		}
	}
	req, err := http.NewRequestWithContext(stream.Context(), start.Method, start.Url, &body)
	if err != nil {
		return err
	}
	for key, values := range start.Headers {
		req.Header[key] = values.Values
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	headers := make(map[string]*pluginv1.HeaderValues, len(resp.Header))
	for key, values := range resp.Header {
		headers[key] = &pluginv1.HeaderValues{Values: values}
	}
	if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{StatusCode: int32(resp.StatusCode), Headers: headers, ContentLength: resp.ContentLength}}}); err != nil {
		return err
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: buf[:n]}}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func main() { pluginv1.Serve(transport{}) }
