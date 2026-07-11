package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
)

type RequestBodyTooLargeError struct{ Limit int64 }

func (e *RequestBodyTooLargeError) Error() string {
	return fmt.Sprintf("request body exceeds %d bytes", e.Limit)
}

var ErrUnsupportedContentEncoding = errors.New("unsupported content encoding")

func ReadLimitedRequestBody(body io.Reader, encoding string, limit int64) ([]byte, error) {
	if limit < 1 {
		return nil, errors.New("invalid request body limit")
	}
	req, err := http.NewRequest(http.MethodPost, "http://resource-protection.local", io.NopCloser(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Encoding", encoding)
	result, err := pkghttputil.ReadRequestBodyWithLimit(req, limit)
	if err == nil {
		return result, nil
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return nil, &RequestBodyTooLargeError{Limit: maxErr.Limit}
	}
	if errors.Is(err, pkghttputil.ErrUnsupportedContentEncoding) {
		return nil, ErrUnsupportedContentEncoding
	}
	return nil, err
}
