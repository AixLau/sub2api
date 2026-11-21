package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

type openAIHTTP2StreamFailureRecorder interface {
	RecordOpenAIHTTP2StreamFailure(proxyURL string, err error)
}

func (s *OpenAIGatewayService) recordOpenAIHTTP2StreamFailure(proxyURL string, err error) {
	if s == nil || s.httpUpstream == nil {
		return
	}
	recorder, ok := s.httpUpstream.(openAIHTTP2StreamFailureRecorder)
	if !ok {
		return
	}
	recordErr := err
	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) && failoverErr != nil && len(failoverErr.ResponseBody) > 0 {
		// UpstreamFailoverError.Error intentionally exposes only a stable status;
		// the sanitized body carries the transport marker needed by the repository
		// fallback classifier.
		recordErr = errors.New(string(failoverErr.ResponseBody))
	}
	recorder.RecordOpenAIHTTP2StreamFailure(proxyURL, recordErr)
}

// isOpenAIHTTP2StreamResetText matches the transport signatures of an HTTP/2
// stream or connection reset in a raw (unsanitized) error text.
func isOpenAIHTTP2StreamResetText(text string) bool {
	message := strings.ToLower(text)
	for _, marker := range []string{
		"stream error: stream id",
		"http2: client connection lost",
		"http2: stream",
		"goaway",
		"refused_stream",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// isOpenAIHTTP2StreamReadFailover reports the narrow transport failure that is
// safe to retry before semantic output reaches the client. HTTP/2 stream resets
// arrive after the response headers, so they bypass the HTTP client's initial
// request error path and need an explicit recovery attempt in the streaming
// forward loop.
func isOpenAIHTTP2StreamReadFailover(err error) bool {
	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) || failoverErr == nil {
		return false
	}
	return isOpenAIHTTP2StreamResetText(string(failoverErr.ResponseBody))
}

// openAIChatPreOutputStreamReadError marks a Chat Completions stream read
// failure that happened before any semantic chunk (content, reasoning, tool
// call, finish or usage) reached the client. It wraps the
// classified stream read error transparently, so callers that only inspect the
// stable client code keep working.
type openAIChatPreOutputStreamReadError struct {
	err error
	// resume is set when a role-only preamble already reached the client; the
	// replay must continue that stream instead of starting a new one.
	resume *openAIChatStreamResume
}

// openAIChatStreamResume carries what the client has already seen of a Chat
// Completions stream across a replay attempt.
type openAIChatStreamResume struct {
	ID       string
	Created  int64
	SentRole bool
}

const openAIChatStreamResumeKey = "openai_chat_stream_resume"

func setOpenAIChatStreamResume(c *gin.Context, resume *openAIChatStreamResume) {
	if c == nil || resume == nil {
		return
	}
	c.Set(openAIChatStreamResumeKey, resume)
}

// takeOpenAIChatStreamResume consumes the resume state so it applies to exactly
// one subsequent streaming attempt.
func takeOpenAIChatStreamResume(c *gin.Context) *openAIChatStreamResume {
	if c == nil {
		return nil
	}
	raw, ok := c.Get(openAIChatStreamResumeKey)
	if !ok {
		return nil
	}
	delete(c.Keys, openAIChatStreamResumeKey)
	resume, _ := raw.(*openAIChatStreamResume)
	return resume
}

// chatChunkCarriesSemanticOutput reports whether a chunk carries anything
// beyond the role-only preamble.
func chatChunkCarriesSemanticOutput(chunk apicompat.ChatCompletionsChunk) bool {
	if chunk.Usage != nil {
		return true
	}
	for _, choice := range chunk.Choices {
		delta := choice.Delta
		if choice.FinishReason != nil || delta.Content != nil || delta.Refusal != nil ||
			delta.ReasoningContent != nil || delta.Reasoning != nil || len(delta.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func (e *openAIChatPreOutputStreamReadError) Error() string { return e.err.Error() }

func (e *openAIChatPreOutputStreamReadError) Unwrap() error { return e.err }

// openAIChatPreOutputHTTP2StreamResetCause returns the raw transport error when
// err is a pre-output HTTP/2 stream reset on the Chat Completions stream path.
// The raw error never reaches the client; it only drives the retry decision and
// the proxy-scoped HTTP/1.1 fallback window.
func openAIChatPreOutputHTTP2StreamResetCause(err error) (error, *openAIChatStreamResume, bool) {
	var preOutput *openAIChatPreOutputStreamReadError
	if !errors.As(err, &preOutput) || preOutput == nil {
		return nil, nil, false
	}
	var readErr *openAIUpstreamStreamReadError
	if !errors.As(preOutput.err, &readErr) || readErr == nil || readErr.cause == nil {
		return nil, nil, false
	}
	if !isOpenAIHTTP2StreamResetText(readErr.cause.Error()) {
		return nil, nil, false
	}
	return readErr.cause, preOutput.resume, true
}

type openAIChatHTTP2StreamRetryContextKey struct{}

// markOpenAIChatHTTP2StreamRetry records that the Chat Completions forward has
// already replayed once after an HTTP/2 reset, carrying the original start time
// so latency and first-token metrics still measure the whole client request.
func markOpenAIChatHTTP2StreamRetry(ctx context.Context, startTime time.Time) context.Context {
	return context.WithValue(ctx, openAIChatHTTP2StreamRetryContextKey{}, startTime)
}

func openAIChatHTTP2StreamRetryStart(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Time{}, false
	}
	startTime, ok := ctx.Value(openAIChatHTTP2StreamRetryContextKey{}).(time.Time)
	return startTime, ok
}
