package pluginv1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestPrepareOutboundRoundTripPreservesHookDecision(t *testing.T) {
	in := &PrepareOutboundRequest{
		RequestId: "req-1",
		Method:    "POST",
		Url:       "https://api.openai.com/v1/responses",
		Host:      "api.openai.com",
		Headers: map[string]*HeaderValues{
			"accept": {Values: []string{"text/event-stream"}},
		},
		AccountId:   42,
		Model:       "gpt-5",
		Transport:   "sse",
		Platform:    "openai",
		AccountType: "oauth",
		HasBody:     true,
	}
	out := &PrepareOutboundResponse{
		Action: HeaderHookAction_HEADER_HOOK_ACTION_CONTINUE,
		HeadersToSet: map[string]*HeaderValues{
			"x-codex-turn-state": {Values: []string{"ticket-1"}},
		},
		HeadersToDelete: []string{"x-stale-ticket"},
		ReasonCode:      "ticket_refreshed",
		RetryAfterMs:    1000,
	}

	encoded, err := proto.Marshal(in)
	require.NoError(t, err)
	decodedIn := new(PrepareOutboundRequest)
	require.NoError(t, proto.Unmarshal(encoded, decodedIn))
	require.Equal(t, in.GetRequestId(), decodedIn.GetRequestId())
	require.Equal(t, in.GetHeaders()["accept"].GetValues(), decodedIn.GetHeaders()["accept"].GetValues())

	encoded, err = proto.Marshal(out)
	require.NoError(t, err)
	decodedOut := new(PrepareOutboundResponse)
	require.NoError(t, proto.Unmarshal(encoded, decodedOut))
	require.Equal(t, HeaderHookAction_HEADER_HOOK_ACTION_CONTINUE, decodedOut.GetAction())
	require.Equal(t, []string{"ticket-1"}, decodedOut.GetHeadersToSet()["x-codex-turn-state"].GetValues())
	require.Equal(t, []string{"x-stale-ticket"}, decodedOut.GetHeadersToDelete())
}

func TestObserveOutboundResponseRoundTripPreservesTicketFeedback(t *testing.T) {
	in := &ObserveOutboundResponseRequest{
		RequestId:       "req-2",
		AccountId:       42,
		Model:           "gpt-5",
		Transport:       "websocket",
		StatusCode:      200,
		Status:          "200 OK",
		Headers:         map[string]*HeaderValues{"set-cookie": {Values: []string{"session=redacted"}}},
		RequestSent:     true,
		SentTicketState: "ticket-1",
		Platform:        "openai",
		AccountType:     "oauth",
	}
	out := &ObserveOutboundResponseResponse{Observed: true, ReasonCode: "ticket_rotated"}

	encoded, err := proto.Marshal(in)
	require.NoError(t, err)
	decodedIn := new(ObserveOutboundResponseRequest)
	require.NoError(t, proto.Unmarshal(encoded, decodedIn))
	require.Equal(t, in.GetSentTicketState(), decodedIn.GetSentTicketState())
	require.Equal(t, in.GetHeaders()["set-cookie"].GetValues(), decodedIn.GetHeaders()["set-cookie"].GetValues())

	encoded, err = proto.Marshal(out)
	require.NoError(t, err)
	decodedOut := new(ObserveOutboundResponseResponse)
	require.NoError(t, proto.Unmarshal(encoded, decodedOut))
	require.True(t, decodedOut.GetObserved())
	require.Equal(t, "ticket_rotated", decodedOut.GetReasonCode())
}

func TestTransportPluginServiceIncludesHeaderHooks(t *testing.T) {
	methods := map[string]bool{}
	for _, method := range TransportPlugin_ServiceDesc.Methods {
		methods[method.MethodName] = true
	}
	require.True(t, methods["PrepareOutbound"])
	require.True(t, methods["ObserveOutboundResponse"])

	descriptorMethods := map[string]bool{}
	service := File_plugin_proto.Services().ByName("TransportPlugin")
	require.NotNil(t, service)
	for i := 0; i < service.Methods().Len(); i++ {
		descriptorMethods[string(service.Methods().Get(i).Name())] = true
	}
	require.True(t, descriptorMethods["PrepareOutbound"])
	require.True(t, descriptorMethods["ObserveOutboundResponse"])
}
