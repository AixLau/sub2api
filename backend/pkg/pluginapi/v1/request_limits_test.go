package pluginv1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func TestRequestBodyLimitRoundTrip(t *testing.T) {
	original := metadata.Pairs(RequestBodyLimitMetadataKey, "1", "trace", "preserved")
	ctx, err := WithRequestBodyLimit(metadata.NewOutgoingContext(context.Background(), original), 173<<20)
	require.NoError(t, err)
	md, _ := metadata.FromOutgoingContext(ctx)
	require.Equal(t, []string{"preserved"}, md.Get("trace"))
	require.Equal(t, []string{"1"}, original.Get(RequestBodyLimitMetadataKey))
	limit, err := RequestBodyLimit(metadata.NewIncomingContext(context.Background(), md))
	require.NoError(t, err)
	require.Equal(t, int64(173<<20), limit)
}

func TestRequestBodyLimitRejectsMissingOrInvalidPolicy(t *testing.T) {
	for _, values := range [][]string{nil, {"0"}, {"-1"}, {"invalid"}, {"9223372036854775808"}, {"10", "20"}} {
		_, err := RequestBodyLimit(metadata.NewIncomingContext(context.Background(), metadata.MD{RequestBodyLimitMetadataKey: values}))
		require.Error(t, err)
	}
	for _, limit := range []int64{0, -1} {
		_, err := WithRequestBodyLimit(context.Background(), limit)
		require.Error(t, err)
	}
}
