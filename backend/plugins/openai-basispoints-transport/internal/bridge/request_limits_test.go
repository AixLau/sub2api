package bridge

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareUsesHostRequestBodyLimit(t *testing.T) {
	raw := encoded(map[string]string{"input": "hello"})
	for _, limit := range []int64{0, -1, int64(len(raw) - 1)} {
		_, err := Prepare(context.Background(), raw, "limit", memoryStore{}, nil, limit)
		require.ErrorContains(t, err, "gateway.max_body_size")
	}
	baseline, err := Prepare(context.Background(), raw, "limit", memoryStore{}, nil, 256<<20)
	require.NoError(t, err)
	require.Greater(t, len(baseline.Body), len(raw))
	_, err = Prepare(context.Background(), raw, "limit", memoryStore{}, nil, int64(len(baseline.Body)-1))
	require.ErrorContains(t, err, "转换后请求体")
	_, err = Prepare(context.Background(), raw, "limit", memoryStore{}, nil, int64(len(baseline.Body)))
	require.NoError(t, err)
}

func TestPrepareAcceptsRequestAboveFormer64MiBLimit(t *testing.T) {
	raw := append(bytes.Repeat([]byte(" "), 64<<20), encoded(map[string]string{"input": "hello"})...)
	r, err := Prepare(context.Background(), raw, "large", memoryStore{}, nil, 128<<20)
	require.NoError(t, err)
	require.Contains(t, string(r.Body), "hello")
	_, err = Prepare(context.Background(), raw, "large", memoryStore{}, nil, 64<<20)
	require.ErrorContains(t, err, "67108864 字节")
}

func TestToolFeedbackUsesSameRequestBodyLimit(t *testing.T) {
	r := prepareWeather(t, memoryStore{})
	r.maxRequestBodyBytes = int64(len(r.Body))
	r.Feedback = func(context.Context, []byte) ([]byte, error) {
		t.Fatal("oversized continuation must not reach upstream")
		return nil, nil
	}
	_, err := r.Response(context.Background(), encoded(feedbackResponse("limit", feedbackCall("run_officejs", "invalid payload", "limit"))))
	require.ErrorContains(t, err, "工具反馈请求超过宿主 gateway.max_body_size")
}
