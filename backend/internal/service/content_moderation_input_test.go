package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExtractContentModerationInput_AnthropicAgentToolLoopScansClientToolResult(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"调用一下天气工具"},
			{"role":"assistant","content":[{"type":"tool_use","id":"tool_1","name":"weather","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"晴 25 度"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "调用一下天气工具 weather 晴 25 度", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_AnthropicFirstTurnExtractsUser(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"Q1"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "Q1", input.Text)
}

func TestExtractContentModerationInput_AnthropicMultiTurnExtractsAllClientContextText(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"Q1"},
			{"role":"assistant","content":"A1"},
			{"role":"user","content":"Q2"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "Q1 A1 Q2", input.Text)
}

func TestExtractContentModerationInput_AnthropicStreamResendExtractsClientContextText(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"原问题"},
			{"role":"assistant","content":"部分回答……"},
			{"role":"user","content":"重发"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "原问题 部分回答…… 重发", input.Text)
}

func TestExtractContentModerationInput_OpenAIMessagesUsesAnthropicMessageShape(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"调用一下天气工具"},
			{"role":"assistant","content":[{"type":"tool_use","id":"tool_1","name":"weather","input":{"city":"上海"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"晴 25 度"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIMessages, body)

	require.Equal(t, "调用一下天气工具 weather city 上海 晴 25 度", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_OpenAIChatAgentToolLoopScansClientToolOutput(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"system","content":"sys"},
			{"role":"user","content":"列出我的订单"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"orders","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"[]"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, "sys 列出我的订单 []", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_OpenAIEmbeddingsScansInputText(t *testing.T) {
	body := []byte(`{
		"model": "text-embedding-3-small",
		"input": [
			"plain embedding text",
			{"text": "object text value"},
			{"metadata": {"filename": "risk-file.txt"}}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIEmbeddings, body)

	require.Contains(t, input.Text, "plain embedding text")
	require.Contains(t, input.Text, "object text value")
	require.Contains(t, input.Text, "risk-file.txt")
	require.NotEmpty(t, input.Sources)
	require.Equal(t, "openai_embeddings.input", input.Sources[0].Source)
}

func TestExtractContentModerationInput_OpenAIImagesKeepsImageFieldTextAfterDedup(t *testing.T) {
	body := []byte(`{
		"prompt": "生成图片",
		"images": [
			{"type": "input_text", "text": "参考文件名 risk-image.png"},
			{"image_url": {"url": "https://example.com/risk-image.png"}}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIImages, body)

	require.Contains(t, input.Text, "生成图片")
	require.Contains(t, input.Text, "参考文件名 risk-image.png")
	require.Len(t, input.Sources, 2)
	require.Equal(t, "image.prompt", input.Sources[0].Source)
	require.Equal(t, "image.images", input.Sources[1].Source)
	require.Equal(t, []string{"https://example.com/risk-image.png"}, input.Images)
}

func TestExtractContentModerationInput_OpenAIChatMultiTurnExtractsAllClientContextText(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"Q1"},
			{"role":"assistant","content":"A1"},
			{"role":"user","content":"Q2"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, "Q1 A1 Q2", input.Text)
}

func TestExtractContentModerationInput_GeminiAgentToolLoopScansClientFunctionResponse(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"查询天气"}]},
			{"role":"model","parts":[{"functionCall":{"name":"weather","args":{}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"weather","response":{"text":"晴 25 度"}}}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Equal(t, "查询天气 weather text 晴 25 度", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_GeminiFirstTurnExtractsUser(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"你好"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Equal(t, "你好", input.Text)
}

func TestExtractContentModerationInput_GeminiMultiTurnExtractsAllClientContextText(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"Q1"}]},
			{"role":"model","parts":[{"text":"A1"}]},
			{"role":"user","parts":[{"text":"Q2"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Equal(t, "Q1 A1 Q2", input.Text)
}

func TestExtractContentModerationInput_ResponsesAgentToolLoopScansClientToolOutput(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"运行测试"}]},
			{"type":"function_call","call_id":"call_1","name":"run_tests","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"all passed"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "运行测试 all passed", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_ResponsesAllClientContextMessagesExtracted(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"latest"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "first answer latest", input.Text)
}

func TestExtractContentModerationInput_ResponsesLastAssistantKeepsEarlierClientContextText(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"q1"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a1"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "q1 a1", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_ResponsesScansTopLevelInstructions(t *testing.T) {
	body := []byte(`{
		"model":"gpt-test",
		"instructions":"顶层 instructions 里的风险短语",
		"input":"普通输入"
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "顶层 instructions 里的风险短语")
	require.Contains(t, input.Text, "普通输入")
}

func TestExtractContentModerationInput_OpenAIChatScansToolsFunctionsAndCallArguments(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","content":"继续"},
			{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"tool call arguments 里的风险短语"}}]},
			{"role":"assistant","function_call":{"name":"legacy_lookup","arguments":"function call arguments 里的风险短语"}}
		],
		"tools":[{"type":"function","function":{
			"name":"risk_tool",
			"description":"tool description 里的风险短语",
			"parameters":{"type":"object","properties":{"query":{"description":"schema description 里的风险短语"}}}
		}}],
		"functions":[{"name":"legacy_tool","description":"legacy function 里的风险短语"}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "继续")
	require.Contains(t, input.Text, "tool call arguments 里的风险短语")
	require.Contains(t, input.Text, "function call arguments 里的风险短语")
	require.Contains(t, input.Text, "tool description 里的风险短语")
	require.Contains(t, input.Text, "schema description 里的风险短语")
	require.Contains(t, input.Text, "legacy function 里的风险短语")
}

func TestExtractContentModerationInput_OpenAIChatScansTopLevelInstructions(t *testing.T) {
	body := []byte(`{
		"instructions":"chat 顶层 instructions 里的风险短语",
		"messages":[{"role":"user","content":"普通输入"}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "chat 顶层 instructions 里的风险短语")
	require.Contains(t, input.Text, "普通输入")
}

func TestExtractContentModerationInput_OpenAIChatScansResponseFormatSchema(t *testing.T) {
	body := []byte(`{
		"messages":[{"role":"user","content":"继续"}],
		"response_format":{"type":"json_schema","json_schema":{"name":"risk_schema","description":"chat response schema 里的风险短语","schema":{"properties":{"answer":{"description":"chat schema property 里的风险短语"}}}}}
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "chat response schema 里的风险短语")
	require.Contains(t, input.Text, "chat schema property 里的风险短语")
}

func TestExtractContentModerationInput_ModelVisibleJSONScansDataFields(t *testing.T) {
	encodedRiskText := base64.StdEncoding.EncodeToString([]byte("base64 解码后的风险短语"))
	longEncodedRiskText := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("长 base64 字段解码后的风险短语", 30)))
	textDataURI := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("text data uri 解码后的风险短语"))
	escapedTextDataURI := "data:text/plain,%E9%9D%9Ebase64%20text%20data%20uri%20%E8%A7%A3%E7%A0%81%E5%90%8E%E7%9A%84%E9%A3%8E%E9%99%A9%E7%9F%AD%E8%AF%AD"
	body := []byte(`{
		"messages":[
			{"role":"user","content":"继续"},
			{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{\"data\":\"tool arguments data 字段里的风险短语\",\"image\":\"tool arguments image 字段里的风险短语\",\"file\":\"tool arguments file 字段里的风险短语\",\"base64\":\"tool arguments base64 字段里的风险短语\",\"metadata\":{\"data\":{\"source\":\"form\",\"query\":\"object data source 里的风险短语\"}},\"attachment\":{\"file\":{\"url\":\"https://example.com/a.png\",\"caption\":\"file caption 里的风险短语\"}},\"encoded\":\"` + encodedRiskText + `\",\"media\":{\"file\":{\"mime_type\":\"image/png\",\"data\":\"iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB\",\"caption\":\"mime data caption 里的风险短语\"},\"image\":{\"media_type\":\"image/png\",\"base64\":\"iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB\",\"description\":\"mime base64 description 里的风险短语\"},\"files\":[{\"mime_type\":\"application/pdf\",\"bytes\":\"JVBERi0xLjQK\",\"title\":\"bytes title 里的风险短语\"}]},\"text_mime\":{\"plain\":{\"mime_type\":\"text/plain\",\"data\":\"text plain mime data 里的风险短语\"},\"json\":{\"mime_type\":\"application/json\",\"data\":\"{\\\"prompt\\\":\\\"application json data 里的风险短语\\\"}\"},\"problem\":{\"media_type\":\"application/problem+json\",\"data\":\"{\\\"detail\\\":\\\"problem json data 里的风险短语\\\"}\"}},\"encoded_fields\":{\"base64\":\"` + longEncodedRiskText + `\",\"data\":\"` + textDataURI + `\",\"escaped_data\":\"` + escapedTextDataURI + `\"}}"}}]}
		],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"data":{"description":"schema data 字段里的风险短语"},"image":{"description":"schema image 字段里的风险短语"},"file":{"description":"schema file 字段里的风险短语"},"base64":{"description":"schema base64 字段里的风险短语"}}}}}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "tool arguments data 字段里的风险短语")
	require.Contains(t, input.Text, "tool arguments image 字段里的风险短语")
	require.Contains(t, input.Text, "tool arguments file 字段里的风险短语")
	require.Contains(t, input.Text, "tool arguments base64 字段里的风险短语")
	require.Contains(t, input.Text, "schema data 字段里的风险短语")
	require.Contains(t, input.Text, "schema image 字段里的风险短语")
	require.Contains(t, input.Text, "schema file 字段里的风险短语")
	require.Contains(t, input.Text, "schema base64 字段里的风险短语")
	require.Contains(t, input.Text, "object data source 里的风险短语")
	require.Contains(t, input.Text, "file caption 里的风险短语")
	require.Contains(t, input.Text, "base64 解码后的风险短语")
	require.Contains(t, input.Text, "mime data caption 里的风险短语")
	require.Contains(t, input.Text, "mime base64 description 里的风险短语")
	require.Contains(t, input.Text, "bytes title 里的风险短语")
	require.Contains(t, input.Text, "长 base64 字段解码后的风险短语")
	require.Contains(t, input.Text, "text data uri 解码后的风险短语")
	require.Contains(t, input.Text, "非base64 text data uri 解码后的风险短语")
	require.Contains(t, input.Text, "text plain mime data 里的风险短语")
	require.Contains(t, input.Text, "application json data 里的风险短语")
	require.Contains(t, input.Text, "problem json data 里的风险短语")
}

func TestExtractContentModerationInput_Base64DecodeSkipsOversizePayload(t *testing.T) {
	oversizeEncoded := strings.Repeat("A", maxBase64DecodeInputBytes+4)
	body := []byte(`{
		"messages":[
			{"role":"user","content":"继续"},
			{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{\"base64\":\"` + oversizeEncoded + `\",\"caption\":\"oversize 同级文本风险短语\"}"}}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "oversize 同级文本风险短语")
	require.NotContains(t, input.Text, oversizeEncoded)
	require.True(t, input.Truncated)
	require.Contains(t, input.TruncateReasons, "oversized_base64_skipped")
}

func TestExtractContentModerationInput_ResponsesScansToolsAndFunctionCallArguments(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]},
			{"type":"function_call","name":"lookup","arguments":"responses function call arguments 里的风险短语"}
		],
		"tools":[{"type":"function","name":"lookup","description":"responses tool description 里的风险短语","parameters":{"properties":{"query":{"description":"responses schema description 里的风险短语"}}}}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "继续")
	require.Contains(t, input.Text, "responses function call arguments 里的风险短语")
	require.Contains(t, input.Text, "responses tool description 里的风险短语")
	require.Contains(t, input.Text, "responses schema description 里的风险短语")
}

func TestExtractContentModerationInput_ResponsesScansTextFormatSchema(t *testing.T) {
	body := []byte(`{
		"input":"继续",
		"text":{"format":{"type":"json_schema","name":"risk_schema","description":"responses text format 里的风险短语","schema":{"properties":{"answer":{"description":"responses schema property 里的风险短语"}}}}},
		"response_format":{"type":"json_schema","json_schema":{"description":"responses compat response_format 里的风险短语"}}
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "responses text format 里的风险短语")
	require.Contains(t, input.Text, "responses schema property 里的风险短语")
	require.Contains(t, input.Text, "responses compat response_format 里的风险短语")
}

func TestExtractContentModerationInput_ChaosDeepSchemaSeedCorpus(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]}],
		"tools":[{
			"type":"function",
			"name":"upload",
			"parameters":` + buildNestedSchemaJSON(4, "deep schema chaos 风险短语") + `
		}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "deep schema chaos 风险短语")
	require.False(t, input.Truncated)
}

func TestExtractContentModerationInput_ChaosMixedEncodingPayloadSeedCorpus(t *testing.T) {
	plainEncoded := base64.StdEncoding.EncodeToString([]byte("mixed base64 chaos 风险短语"))
	jsonDataURI := "data:application/problem+json;base64," + base64.StdEncoding.EncodeToString([]byte(`{"detail":"mixed json data uri chaos 风险短语"}`))
	yamlEscapedDataURI := "data:application/yaml,detail:%20mixed%20yaml%20data%20uri%20chaos%20%E9%A3%8E%E9%99%A9%E7%9F%AD%E8%AF%AD"
	body := []byte(`{
		"messages":[{
			"role":"assistant",
			"tool_calls":[{
				"type":"function",
				"function":{
					"name":"inspect",
					"arguments":"{\"payloads\":[{\"base64\":\"` + plainEncoded + `\"},{\"data\":\"` + jsonDataURI + `\"},{\"data\":\"` + yamlEscapedDataURI + `\"}],\"file\":{\"mime_type\":\"text/csv\",\"data\":\"name,notes\\nrow,mixed csv chaos 风险短语\"}}"
				}
			}]
		}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "mixed base64 chaos 风险短语")
	require.Contains(t, input.Text, "mixed json data uri chaos 风险短语")
	require.Contains(t, input.Text, "mixed yaml data uri chaos 风险短语")
	require.Contains(t, input.Text, "mixed csv chaos 风险短语")
	require.False(t, input.Truncated)
}

func TestExtractContentModerationInput_ChaosToolRecursionStressSeedCorpus(t *testing.T) {
	body := []byte(`{
		"input":[{
			"type":"function_call_output",
			"call_id":"call_1",
			"output":` + buildNestedObjectJSON(maxToolResultTextDepth+3, "too deep chaos marker") + `
		}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.True(t, input.Truncated)
	require.Contains(t, input.TruncateReasons, "max_depth")
	require.NotContains(t, input.Text, "too deep chaos marker")
}

func FuzzExtractContentModerationInput_ChaosCorpus(f *testing.F) {
	f.Add(ContentModerationProtocolOpenAIResponses, `{"tools":[{"type":"function","parameters":`+buildNestedSchemaJSON(4, "fuzz deep schema seed")+`}],"input":"hello"}`)
	f.Add(ContentModerationProtocolOpenAIChat, `{"messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"arguments":"{\"data\":\"data:text/plain,%66%75%7a%7a%20data%20uri%20seed\"}"}}]}]}`)
	f.Add(ContentModerationProtocolAnthropicMessages, `{"messages":[{"role":"assistant","content":[{"type":"tool_use","name":"upload","input":{"file":{"mime_type":"text/plain","data":"fuzz text mime seed"}}}]}]}`)
	f.Add(ContentModerationProtocolGemini, `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"query":"fuzz gemini function seed"}}}]}]}`)
	f.Add(ContentModerationProtocolOpenAIResponses, `{"input":[{"type":"function_call_output","output":`+buildNestedObjectJSON(maxToolResultTextDepth+3, "fuzz too deep marker")+`}]}`)

	f.Fuzz(func(t *testing.T, protocol string, body string) {
		if len(body) > maxBase64DecodeInputBytes {
			body = body[:maxBase64DecodeInputBytes]
		}
		input := ExtractContentModerationInput(protocol, []byte(body))
		require.LessOrEqual(t, len([]rune(input.Text)), maxModerationInputRunes)
		require.LessOrEqual(t, len(input.Images), maxContentModerationInputImages)
		if len(input.TruncateReasons) > 0 {
			require.True(t, input.Truncated)
		}
	})
}

func TestExtractContentModerationInput_AnthropicScansToolDeclarations(t *testing.T) {
	body := []byte(`{
		"messages":[{"role":"user","content":[{"type":"text","text":"继续"}]}],
		"tools":[{"name":"lookup","description":"anthropic tool description 里的风险短语","input_schema":{"properties":{"query":{"description":"anthropic schema description 里的风险短语"}}}}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Contains(t, input.Text, "继续")
	require.Contains(t, input.Text, "anthropic tool description 里的风险短语")
	require.Contains(t, input.Text, "anthropic schema description 里的风险短语")
}

func TestExtractContentModerationInput_AnthropicScansToolUseInput(t *testing.T) {
	body := []byte(`{
		"messages":[{"role":"assistant","content":[{"type":"tool_use","name":"lookup","input":{"query":"anthropic tool_use input 里的风险短语"}}]}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Contains(t, input.Text, "lookup")
	require.Contains(t, input.Text, "anthropic tool_use input 里的风险短语")
}

func TestExtractContentModerationInput_AnthropicScansOutputFormatSchema(t *testing.T) {
	body := []byte(`{
		"messages":[{"role":"user","content":[{"type":"text","text":"继续"}]}],
		"output_format":{"type":"json_schema","schema":{"properties":{"answer":{"description":"anthropic output schema 里的风险短语"}}}}
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Contains(t, input.Text, "anthropic output schema 里的风险短语")
}

func TestExtractContentModerationInput_GeminiScansToolDeclarations(t *testing.T) {
	body := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"继续"}]}],
		"tools":[{"functionDeclarations":[{"name":"lookup","description":"gemini tool description 里的风险短语","parameters":{"properties":{"query":{"description":"gemini schema description 里的风险短语"}}}}]}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Contains(t, input.Text, "继续")
	require.Contains(t, input.Text, "gemini tool description 里的风险短语")
	require.Contains(t, input.Text, "gemini schema description 里的风险短语")
}

func TestExtractContentModerationInput_GeminiScansFunctionCallArgs(t *testing.T) {
	body := []byte(`{
		"contents":[{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"query":"gemini functionCall args 里的风险短语"}}},{"function_call":{"name":"legacy_lookup","args":{"query":"gemini function_call args 里的风险短语"}}}]}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Contains(t, input.Text, "lookup")
	require.Contains(t, input.Text, "gemini functionCall args 里的风险短语")
	require.Contains(t, input.Text, "legacy_lookup")
	require.Contains(t, input.Text, "gemini function_call args 里的风险短语")
}

func TestExtractContentModerationInput_GeminiScansResponseSchema(t *testing.T) {
	body := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"继续"}]}],
		"generationConfig":{"responseSchema":{"properties":{"answer":{"description":"gemini response schema 里的风险短语"}}},"responseJsonSchema":{"properties":{"json":{"description":"gemini response json schema 里的风险短语"}}}},
		"generation_config":{"response_schema":{"properties":{"legacy":{"description":"gemini legacy response schema 里的风险短语"}}},"response_json_schema":{"properties":{"legacyJson":{"description":"gemini legacy response json schema 里的风险短语"}}}}
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Contains(t, input.Text, "gemini response schema 里的风险短语")
	require.Contains(t, input.Text, "gemini response json schema 里的风险短语")
	require.Contains(t, input.Text, "gemini legacy response schema 里的风险短语")
	require.Contains(t, input.Text, "gemini legacy response json schema 里的风险短语")
}

func TestAddModerationText_StripsPureSystemReminderBlock(t *testing.T) {
	var parts []string

	addModerationText(&parts, "<system-reminder>工具说明</system-reminder>")

	require.Empty(t, parts)
}

func TestAddModerationText_KeepsUserTextAroundSystemReminderBlock(t *testing.T) {
	var parts []string

	addModerationText(&parts, "用户正文 <system-reminder>工具说明</system-reminder> 风险内容")

	require.Equal(t, []string{"用户正文 风险内容"}, parts)
}

func TestAddModerationText_UnclosedSystemReminderDoesNotDropWholeText(t *testing.T) {
	var parts []string

	addModerationText(&parts, "用户正文 <system-reminder>未闭合 风险内容")

	require.Equal(t, []string{"用户正文 未闭合 风险内容"}, parts)
}

func TestAddModerationText_MultipleSystemReminderBlocksOnlyRemoveMarkers(t *testing.T) {
	var parts []string

	addModerationText(&parts, "A <system-reminder>one</system-reminder> B <system-reminder>two</system-reminder> C")

	require.Equal(t, []string{"A B C"}, parts)
}

func TestExtractContentModerationInput_OpenAIChatScansClientSuppliedToolAndFunctionMessages(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","content":"请根据工具结果继续"},
			{"role":"tool","tool_call_id":"call_1","content":"工具结果包含风险短语"},
			{"role":"function","name":"lookup","content":"函数结果包含另一段风险内容"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "请根据工具结果继续")
	require.Contains(t, input.Text, "工具结果包含风险短语")
	require.Contains(t, input.Text, "函数结果包含另一段风险内容")
}

func TestExtractContentModerationInput_OpenAIChatScansClientSuppliedSystemDeveloperAndAssistantMessages(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"system","content":"系统消息里的风险短语"},
			{"role":"developer","content":"开发者消息里的风险短语"},
			{"role":"assistant","content":"助手历史里的风险短语"},
			{"role":"user","content":"继续"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "系统消息里的风险短语")
	require.Contains(t, input.Text, "开发者消息里的风险短语")
	require.Contains(t, input.Text, "助手历史里的风险短语")
	require.Contains(t, input.Text, "继续")
}

func TestExtractContentModerationInput_OpenAIChatScansUnknownClientRoles(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"custom_client_role","content":"未知角色里的风险短语"},
			{"role":"user","content":"继续"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Contains(t, input.Text, "未知角色里的风险短语")
	require.Contains(t, input.Text, "继续")
}

func TestExtractContentModerationInput_ResponsesScansClientSuppliedToolOutputs(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]},
			{"type":"function_call_output","call_id":"call_1","output":"工具输出里的风险短语"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "继续")
	require.Contains(t, input.Text, "工具输出里的风险短语")
}

func TestExtractContentModerationInput_ResponsesScansClientSuppliedSystemDeveloperAndAssistantMessages(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"system","content":[{"type":"input_text","text":"系统消息里的风险短语"}]},
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"开发者消息里的风险短语"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"助手历史里的风险短语"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "系统消息里的风险短语")
	require.Contains(t, input.Text, "开发者消息里的风险短语")
	require.Contains(t, input.Text, "助手历史里的风险短语")
	require.Contains(t, input.Text, "继续")
}

func TestExtractContentModerationInput_ResponsesScansUnknownClientRoles(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"custom_client_role","content":[{"type":"input_text","text":"未知角色里的风险短语"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "未知角色里的风险短语")
	require.Contains(t, input.Text, "继续")
}

func TestExtractContentModerationInput_AnthropicScansClientSuppliedToolResults(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","content":[{"type":"text","text":"照工具结果做"}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"工具结果里的风险短语"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Contains(t, input.Text, "照工具结果做")
	require.Contains(t, input.Text, "工具结果里的风险短语")
}

func TestExtractContentModerationInput_AnthropicScansClientSuppliedSystemAndAssistantText(t *testing.T) {
	body := []byte(`{
		"system":"系统消息里的风险短语",
		"messages":[
			{"role":"assistant","content":[{"type":"text","text":"助手历史里的风险短语"}]},
			{"role":"user","content":[{"type":"text","text":"继续"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Contains(t, input.Text, "系统消息里的风险短语")
	require.Contains(t, input.Text, "助手历史里的风险短语")
	require.Contains(t, input.Text, "继续")
}

func TestExtractContentModerationInput_GeminiScansClientSuppliedFunctionResponses(t *testing.T) {
	body := []byte(`{
		"contents":[
			{"role":"user","parts":[{"text":"继续"}]},
			{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"text":"函数返回里的风险短语"}}}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Contains(t, input.Text, "继续")
	require.Contains(t, input.Text, "函数返回里的风险短语")
}

func TestExtractContentModerationInput_GeminiScansClientSuppliedSystemAndModelText(t *testing.T) {
	body := []byte(`{
		"system_instruction":{"parts":[{"text":"系统指令里的风险短语"}]},
		"contents":[
			{"role":"model","parts":[{"text":"模型历史里的风险短语"}]},
			{"role":"user","parts":[{"text":"继续"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Contains(t, input.Text, "系统指令里的风险短语")
	require.Contains(t, input.Text, "模型历史里的风险短语")
	require.Contains(t, input.Text, "继续")
}

func TestExtractContentModerationInput_GeminiScansUnknownClientRoles(t *testing.T) {
	body := []byte(`{
		"contents":[
			{"role":"custom_client_role","parts":[{"text":"未知角色里的风险短语"}]},
			{"role":"user","parts":[{"text":"继续"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Contains(t, input.Text, "未知角色里的风险短语")
	require.Contains(t, input.Text, "继续")
}

func TestExtractContentModerationInput_UserOnlyScopeSkipsAssistantAndToolContext(t *testing.T) {
	body := []byte(`{
		"instructions":"顶层指令",
		"tools":[{"type":"function","function":{"name":"lookup","description":"工具定义"}}],
		"response_format":{"type":"json_schema","json_schema":{"description":"结构化输出 schema"}},
		"messages":[
			{"role":"system","content":"系统上下文"},
			{"role":"assistant","content":"助手历史"},
			{"role":"tool","content":"工具结果"},
			{"role":"user","content":"用户新输入"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body, ContentModerationAuditScopeUserOnly)

	require.Equal(t, "用户新输入", input.Text)
	require.Len(t, input.Sources, 1)
	require.Equal(t, "openai_chat.messages[3].role=user.content", input.Sources[0].Source)
}

func TestExtractContentModerationInput_UserAndToolScopeSkipsSystemAndAssistantContext(t *testing.T) {
	body := []byte(`{
		"instructions":"顶层指令",
		"tools":[{"type":"function","function":{"name":"lookup","description":"工具定义"}}],
		"response_format":{"type":"json_schema","json_schema":{"description":"结构化输出 schema"}},
		"messages":[
			{"role":"system","content":"系统上下文"},
			{"role":"assistant","content":"助手历史"},
			{"role":"tool","content":"工具结果"},
			{"role":"user","content":"用户新输入"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body, ContentModerationAuditScopeUserAndTool)

	require.Contains(t, input.Text, "用户新输入")
	require.Contains(t, input.Text, "工具结果")
	require.NotContains(t, input.Text, "系统上下文")
	require.NotContains(t, input.Text, "助手历史")
	require.NotContains(t, input.Text, "顶层指令")
	require.NotContains(t, input.Text, "工具定义")
	require.NotContains(t, input.Text, "结构化输出 schema")
}

func TestExtractContentModerationInput_DeduplicatesRepeatedSourceTextWithinRequest(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"user","content":"重复文本"},
			{"role":"user","content":"重复文本"},
			{"role":"user","content":"新的文本"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, "重复文本 新的文本", input.Text)
	require.Len(t, input.Sources, 2)
}

func TestExtractContentModerationInput_ScansJSONToolResultTextValues(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]},
			{"type":"function_call_output","call_id":"call_1","output":{"risk":"对象字段里的风险短语","score":1}}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "继续")
	require.Contains(t, input.Text, "对象字段里的风险短语")
}

func TestExtractContentModerationInput_ScansJSONToolResultKeys(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]},
			{"type":"function_call_output","call_id":"call_1","output":{"dangerous_instruction_here":true}}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Contains(t, input.Text, "继续")
	require.Contains(t, input.Text, "dangerous_instruction_here")
}

func TestCollectToolResultTextValue_StopsAfterStringLimit(t *testing.T) {
	var builder strings.Builder
	builder.WriteString(`{"values":[`)
	for i := 0; i < maxToolResultTextStrings; i++ {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(`"safe"`)
	}
	builder.WriteString(`,"after-string-limit-marker"]}`)

	var parts []string
	var images []string
	collectToolResultTextValue(gjson.Parse(builder.String()), &parts, &images, 0)

	require.NotContains(t, strings.Join(parts, " "), "after-string-limit-marker")
}

func TestCollectToolResultTextValue_TruncatesLongStringValues(t *testing.T) {
	longValue := strings.Repeat("a", maxToolResultTextStringRunes) + "tail-marker"

	var parts []string
	var images []string
	collectToolResultTextValue(gjson.Parse(`{"value":"`+longValue+`"}`), &parts, &images, 0)

	require.NotContains(t, strings.Join(parts, " "), "tail-marker")
}

func TestCollectToolResultTextValue_StopsAfterTotalRuneLimit(t *testing.T) {
	chunk := strings.Repeat("中", maxToolResultTextStringRunes)
	var builder strings.Builder
	builder.WriteString(`{"values":[`)
	for i := 0; i < (maxToolResultTextTotalRunes/maxToolResultTextStringRunes)+2; i++ {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteByte('"')
		builder.WriteString(chunk)
		builder.WriteByte('"')
	}
	builder.WriteString(`,"after-total-limit-marker"]}`)

	var parts []string
	var images []string
	collectToolResultTextValue(gjson.Parse(builder.String()), &parts, &images, 0)

	require.NotContains(t, strings.Join(parts, " "), "after-total-limit-marker")
}

func TestExtractContentModerationInput_RecordsToolJSONTruncationReason(t *testing.T) {
	nested := `"too deep marker"`
	for i := 0; i < maxToolResultTextDepth+2; i++ {
		nested = `{"nested":` + nested + `}`
	}
	body := []byte(`{"input":[{"type":"function_call_output","call_id":"call_1","output":` + nested + `}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.True(t, input.Truncated)
	require.Contains(t, input.TruncateReasons, "max_depth")
	require.NotContains(t, input.Text, "too deep marker")
}

func buildNestedSchemaJSON(depth int, marker string) string {
	node := map[string]any{
		"type":        "string",
		"description": marker,
	}
	for i := 0; i < depth; i++ {
		node = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"level": node,
			},
		}
	}
	raw, err := json.Marshal(node)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func buildNestedObjectJSON(depth int, marker string) string {
	var node any = marker
	for i := 0; i < depth; i++ {
		node = map[string]any{
			"nested": node,
		}
	}
	raw, err := json.Marshal(node)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
