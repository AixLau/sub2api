package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// 上游托管执行的 tool_search 项 call_id 为 null，Codex 会原样回放进 HTTP 历史，不能被预校验拦截。
const serverToolSearchItems = `{"type":"tool_search_call","execution":"server","call_id":null,"status":"completed","arguments":{"paths":["crm"]}},{"type":"tool_search_output","execution":"server","call_id":null,"status":"completed","tools":[]}`

func TestValidateFunctionCallOutputRequestAllowsServerExecutedToolSearch(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[{"role":"user","content":"hi"},` + serverToolSearchItems + `]}`)
	h := &OpenAIGatewayHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	require.True(t, h.validateFunctionCallOutputRequest(c, body, zap.NewNop()))
	require.False(t, c.Writer.Written())
}

func TestNormalizeCodexDelegationBootstrapIgnoresServerExecutedToolSearch(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"codex_app","name":"send_message_to_thread","output":"` + delegationEnvelope + `"},` + serverToolSearchItems + `]}`)

	got, changed := normalizeCodexDelegationBootstrap(body)
	require.True(t, changed)
	require.Equal(t, "message", gjson.GetBytes(got, "input.0.type").String())
	require.Equal(t, "tool_search_output", gjson.GetBytes(got, "input.2.type").String())
}
