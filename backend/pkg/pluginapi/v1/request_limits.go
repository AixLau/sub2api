package pluginv1

import (
	"context"
	"errors"
	"strconv"

	"google.golang.org/grpc/metadata"
)

// RequestBodyLimitMetadataKey carries the host's gateway.max_body_size policy
// over the private host/plugin RPC. It is never accepted from HTTP headers.
const RequestBodyLimitMetadataKey = "sub2api-request-body-limit"

func WithRequestBodyLimit(ctx context.Context, limit int64) (context.Context, error) {
	if limit <= 0 {
		return nil, errors.New("gateway.max_body_size must be positive")
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(RequestBodyLimitMetadataKey, strconv.FormatInt(limit, 10))
	return metadata.NewOutgoingContext(ctx, md), nil
}

func RequestBodyLimit(ctx context.Context) (int64, error) {
	values := metadata.ValueFromIncomingContext(ctx, RequestBodyLimitMetadataKey)
	if len(values) != 1 {
		return 0, errors.New("宿主未提供唯一的 gateway.max_body_size 请求体限制")
	}
	limit, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || limit <= 0 {
		return 0, errors.New("宿主 gateway.max_body_size 请求体限制无效")
	}
	return limit, nil
}
