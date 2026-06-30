package moderationcoverage

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRuntimeRouteMetaHelpersRoundTripNormalizedMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	SetRouteMeta(c, Entry{
		Method:             " post ",
		Path:               " /v1/responses ",
		Handler:            " OpenAIGatewayHandler.Responses ",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           " openai_responses ",
		Pipeline:           " OPENAI_HTTP ",
		Status:             " COVERED ",
	})

	meta, ok := RouteMetaFromContext(c)
	require.True(t, ok)
	require.Equal(t, "POST", meta.Method)
	require.Equal(t, "/v1/responses", meta.Path)
	require.Equal(t, "OpenAIGatewayHandler.Responses", meta.Handler)
	require.Equal(t, "openai_responses", meta.Protocol)
	require.Equal(t, PipelineOpenAIHTTP, meta.Pipeline)
	require.Equal(t, StatusCovered, meta.Status)
}

func TestPipelineAdmissionHelpersSetBooleanFlagAndMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	MarkPipelineAdmitted(c, " OPENAI_HTTP ", " PRE_FORWARD ", " source ")

	require.True(t, PipelineAdmittedFromContext(c))
	admission, ok := PipelineAdmissionFromContext(c)
	require.True(t, ok)
	require.True(t, admission.Admitted)
	require.Equal(t, PipelineOpenAIHTTP, admission.Pipeline)
	require.Equal(t, StagePreForward, admission.Stage)
	require.Equal(t, "source", admission.Source)
}
