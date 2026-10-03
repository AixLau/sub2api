package service

import "context"

// HTTPUpstreamProfile marks HTTP upstream requests that need provider-specific
// transport policy.
type HTTPUpstreamProfile string

// HTTPUpstreamProtocolOverride controls the transport protocol for one
// request attempt. It is intentionally request-scoped so a recovery retry can
// switch away from a failed HTTP/2 connection without changing global config.
type HTTPUpstreamProtocolOverride string

const (
	HTTPUpstreamProfileDefault    HTTPUpstreamProfile = ""
	HTTPUpstreamProfileOpenAI     HTTPUpstreamProfile = "openai"
	HTTPUpstreamProfileGrok       HTTPUpstreamProfile = "grok"
	HTTPUpstreamProfileLongStream HTTPUpstreamProfile = "long_stream"
)

const (
	HTTPUpstreamProtocolOverrideNone  HTTPUpstreamProtocolOverride = ""
	HTTPUpstreamProtocolOverrideHTTP1 HTTPUpstreamProtocolOverride = "http1"
)

type httpUpstreamProfileContextKey struct{}
type httpUpstreamProtocolOverrideContextKey struct{}
type httpUpstreamDisableRedirectsContextKey struct{}
type httpUpstreamPublicHostsOnlyContextKey struct{}

// WithHTTPUpstreamProfile injects an upstream transport profile into ctx.
func WithHTTPUpstreamProfile(ctx context.Context, profile HTTPUpstreamProfile) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if profile == HTTPUpstreamProfileDefault {
		return ctx
	}
	return context.WithValue(ctx, httpUpstreamProfileContextKey{}, profile)
}

// HTTPUpstreamProfileFromContext resolves the upstream transport profile from ctx.
func HTTPUpstreamProfileFromContext(ctx context.Context) HTTPUpstreamProfile {
	if ctx == nil {
		return HTTPUpstreamProfileDefault
	}
	profile, ok := ctx.Value(httpUpstreamProfileContextKey{}).(HTTPUpstreamProfile)
	if !ok {
		return HTTPUpstreamProfileDefault
	}
	switch profile {
	case HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileGrok, HTTPUpstreamProfileLongStream:
		return profile
	default:
		return HTTPUpstreamProfileDefault
	}
}

// WithHTTPUpstreamProtocolOverride forces one upstream request attempt to use
// a specific protocol mode. The override only applies to profiles that support
// the selected transport; other upstreams keep their normal policy.
func WithHTTPUpstreamProtocolOverride(ctx context.Context, override HTTPUpstreamProtocolOverride) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if override == HTTPUpstreamProtocolOverrideNone {
		return ctx
	}
	return context.WithValue(ctx, httpUpstreamProtocolOverrideContextKey{}, override)
}

// HTTPUpstreamProtocolOverrideFromContext resolves a request-scoped protocol
// override without exposing arbitrary transport strings to the repository.
func HTTPUpstreamProtocolOverrideFromContext(ctx context.Context) HTTPUpstreamProtocolOverride {
	if ctx == nil {
		return HTTPUpstreamProtocolOverrideNone
	}
	override, ok := ctx.Value(httpUpstreamProtocolOverrideContextKey{}).(HTTPUpstreamProtocolOverride)
	if !ok {
		return HTTPUpstreamProtocolOverrideNone
	}
	switch override {
	case HTTPUpstreamProtocolOverrideHTTP1:
		return override
	default:
		return HTTPUpstreamProtocolOverrideNone
	}
}

// WithHTTPUpstreamRedirectsDisabled prevents credential-bearing probes from
// following redirects through the shared upstream client.
func WithHTTPUpstreamRedirectsDisabled(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, httpUpstreamDisableRedirectsContextKey{}, true)
}

func HTTPUpstreamRedirectsDisabled(ctx context.Context) bool {
	return ctx != nil && ctx.Value(httpUpstreamDisableRedirectsContextKey{}) == true
}

// WithHTTPUpstreamPublicHostsOnly marks a request whose destination, and every
// redirect hop after it, must resolve to a public address. The shared upstream
// client enforces it regardless of the security.url_allowlist configuration;
// use it for fetches whose URL comes from an untrusted upstream response.
func WithHTTPUpstreamPublicHostsOnly(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, httpUpstreamPublicHostsOnlyContextKey{}, true)
}

func HTTPUpstreamPublicHostsOnly(ctx context.Context) bool {
	return ctx != nil && ctx.Value(httpUpstreamPublicHostsOnlyContextKey{}) == true
}
