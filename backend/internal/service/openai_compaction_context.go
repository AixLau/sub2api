package service

import (
	"context"
	"strings"
)

type openAIForwardModelContextKey struct{}

type openAIForwardModel struct {
	model                  string
	responseModel          string
	useCompactModelMapping bool
}

// WithOpenAIForwardModel records the model present in the forwarded request
// body after channel mapping and whether the legacy /responses/compact-only
// model mapping applies. Native remote compaction v2 keeps this false, so
// channel restriction checks follow the same model chain used by Forward.
func WithOpenAIForwardModel(ctx context.Context, forwardModel string, useCompactModelMapping bool) context.Context {
	return WithOpenAIForwardModelAndResponseModel(ctx, forwardModel, "", useCompactModelMapping)
}

// WithOpenAIForwardModelAndResponseModel records both the model sent in the
// forwarded request and the public model name that should be emitted in the
// response. Channel mapping rewrites the request body before it reaches the
// service, so the public name must travel separately through the context.
func WithOpenAIForwardModelAndResponseModel(ctx context.Context, forwardModel, responseModel string, useCompactModelMapping bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openAIForwardModelContextKey{}, openAIForwardModel{
		model:                  forwardModel,
		responseModel:          responseModel,
		useCompactModelMapping: useCompactModelMapping,
	})
}

func openAIForwardModelFromContext(ctx context.Context) (openAIForwardModel, bool) {
	if ctx == nil {
		return openAIForwardModel{}, false
	}
	forwardModel, ok := ctx.Value(openAIForwardModelContextKey{}).(openAIForwardModel)
	return forwardModel, ok
}

func openAIResponseModelFromContext(ctx context.Context, fallback string) string {
	if forwardModel, ok := openAIForwardModelFromContext(ctx); ok {
		if responseModel := strings.TrimSpace(forwardModel.responseModel); responseModel != "" {
			return responseModel
		}
	}
	return fallback
}
