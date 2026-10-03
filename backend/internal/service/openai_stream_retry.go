package service

import (
	"errors"
	"strings"
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

	message := strings.ToLower(string(failoverErr.ResponseBody))
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
