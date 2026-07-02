package handler

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenAIImagesUseHTTPPreForwardPipelineInHandler(t *testing.T) {
	tests := []struct {
		name                            string
		file                            string
		handler                         string
		protocol                        string
		body                            string
		enableImageStage                string
		imagePermissionBeforeModeration string
		imageEndpoint                   string
	}{
		{
			name:                            "images",
			file:                            "openai_images.go",
			handler:                         "Images",
			protocol:                        "service.ContentModerationProtocolOpenAIImages",
			body:                            "parsed.ModerationBody()",
			enableImageStage:                "true",
			imagePermissionBeforeModeration: "true",
			imageEndpoint:                   "parsed.Endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := openAIHTTPPipelineInputFields(t, tt.file, tt.handler)

			require.Equal(t, tt.protocol, fields["Protocol"])
			require.Equal(t, tt.body, fields["Body"])
			require.Equal(t, "true", fields["SkipCyberStage"])
			if tt.enableImageStage != "" {
				require.Equal(t, tt.enableImageStage, fields["EnableImageStage"])
			}
			if tt.imagePermissionBeforeModeration != "" {
				require.Equal(t, tt.imagePermissionBeforeModeration, fields["ImagePermissionBeforeModeration"])
			}
			if tt.imageEndpoint != "" {
				require.Equal(t, tt.imageEndpoint, fields["ImageEndpoint"])
			}
		})
	}
}

func TestOpenAIChatAndEmbeddingsPreForwardRunThroughGatewayPipelineRegistrar(t *testing.T) {
	tests := []struct {
		name             string
		file             string
		handler          string
		protocolConstant string
	}{
		{
			name:             "chat",
			file:             "openai_chat_completions.go",
			handler:          "ChatCompletions",
			protocolConstant: "ContentModerationProtocolOpenAIChat",
		},
		{
			name:             "embeddings",
			file:             "openai_embeddings.go",
			handler:          "Embeddings",
			protocolConstant: "ContentModerationProtocolOpenAIEmbeddings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.False(t, handlerCallsOpenAIHTTPPreForwardPipeline(t, tt.file, tt.handler),
				"OpenAIGatewayHandler.%s must receive pre-forward admission from GatewayPipelineRegistrar instead of calling runOpenAIHTTPPreForwardPipeline directly", tt.handler)
			require.True(t, gatewaySourceBindsOpenAIHTTPEntrypointForProtocol(t, tt.protocolConstant),
				"OpenAI %s routes must bind OpenAIGatewayHandler.EnterOpenAIHTTPGatewayPipeline through NewGatewayPipelineRegistrar", tt.name)
		})
	}
}

func TestOpenAIImagesAndEmbeddingsPipelineSkipCyberStagePreservesExistingBehavior(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		target   string
		protocol string
		model    string
		body     []byte
	}{
		{
			name:     "images",
			target:   "/v1/images/generations",
			protocol: service.ContentModerationProtocolOpenAIImages,
			model:    "gpt-image-2",
			body:     []byte(`{"model":"gpt-image-2","prompt":"draw"}`),
		},
		{
			name:     "embeddings",
			target:   "/v1/embeddings",
			protocol: service.ContentModerationProtocolOpenAIEmbeddings,
			model:    "text-embedding-3-small",
			body:     []byte(`{"model":"text-embedding-3-small","input":"hello"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, tt.target, strings.NewReader(string(tt.body)))

			guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
				Allowed: true,
				Action:  service.ContentModerationActionAllow,
			}}
			cyberChecker := &openAIChatCyberPipelineCheckerSpy{enabled: true, blocked: true}
			apiKey := &service.APIKey{
				ID:   301,
				Name: "pipeline-key",
				Group: &service.Group{
					ID:                   2,
					AllowImageGeneration: true,
				},
			}
			h := &OpenAIGatewayHandler{
				pipeline: &OpenAIGatewayPipeline{
					moderationGuard:     guard,
					cyberSessionChecker: cyberChecker,
				},
			}

			result := h.runOpenAIHTTPPreForwardPipeline(c, zap.NewNop(), openAIHTTPPreForwardPipelineInput{
				APIKey:         apiKey,
				Subject:        middleware2.AuthSubject{UserID: 7, Concurrency: 1},
				Protocol:       tt.protocol,
				Model:          tt.model,
				Body:           tt.body,
				SkipCyberStage: true,
			})

			require.False(t, result.Blocked)
			require.Nil(t, result.ImageReleaseFunc)
			require.Len(t, guard.calls, 1)
			require.Equal(t, tt.protocol, guard.calls[0].Protocol)
			require.Equal(t, tt.model, guard.calls[0].Model)
			require.Equal(t, tt.body, guard.calls[0].Body)
			require.Zero(t, cyberChecker.runtimeCalls)
			require.Empty(t, cyberChecker.checkedKeys)
		})
	}
}

func openAIHTTPPipelineInputFields(t *testing.T, fileName string, handlerName string) map[string]string {
	t.Helper()

	src, err := os.ReadFile(fileName)
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fileName, src, 0)
	require.NoError(t, err)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != handlerName {
			continue
		}
		var fields map[string]string
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if fields != nil {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "runOpenAIHTTPPreForwardPipeline" {
				return true
			}
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.CompositeLit)
				if !ok {
					continue
				}
				if ident, ok := lit.Type.(*ast.Ident); !ok || ident.Name != "openAIHTTPPreForwardPipelineInput" {
					continue
				}
				fields = compositeLiteralFields(t, fset, lit)
				return false
			}
			return true
		})
		if fields == nil {
			t.Fatalf("%s.%s must call h.runOpenAIHTTPPreForwardPipeline with openAIHTTPPreForwardPipelineInput", fileName, handlerName)
		}
		return fields
	}

	t.Fatalf("%s does not define handler %s", fileName, handlerName)
	return nil
}

func handlerCallsOpenAIHTTPPreForwardPipeline(t *testing.T, fileName string, handlerName string) bool {
	t.Helper()

	src, err := os.ReadFile(fileName)
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fileName, src, 0)
	require.NoError(t, err)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != handlerName {
			continue
		}
		found := false
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if found {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "runOpenAIHTTPPreForwardPipeline" {
				found = true
				return false
			}
			return true
		})
		return found
	}

	t.Fatalf("%s does not define handler %s", fileName, handlerName)
	return false
}

func gatewaySourceBindsOpenAIHTTPEntrypointForProtocol(t *testing.T, protocolConstant string) bool {
	t.Helper()

	src, err := os.ReadFile("../server/routes/gateway.go")
	require.NoError(t, err)
	return strings.Contains(string(src), "NewGatewayPipelineRegistrar") &&
		strings.Contains(string(src), "EnterOpenAIHTTPGatewayPipeline") &&
		strings.Contains(string(src), protocolConstant)
}

func compositeLiteralFields(t *testing.T, fset *token.FileSet, lit *ast.CompositeLit) map[string]string {
	t.Helper()

	fields := make(map[string]string)
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		fields[key.Name] = nodeString(t, fset, kv.Value)
	}
	return fields
}

func nodeString(t *testing.T, fset *token.FileSet, node ast.Node) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, format.Node(&buf, fset, node))
	return buf.String()
}
