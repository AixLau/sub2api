package transport

import (
	"context"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/bridge"
)

// The host owns persistence and scopes this namespace to the installed plugin.
// Values contain only complete tool items/turn metadata, never access tokens.
type hostStateStore struct{ client pluginv1.HostServiceClient }

func (s hostStateStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := s.client.KVGet(ctx, &pluginv1.KVGetRequest{Namespace: "basispoints-tools-v1", Key: key})
	if err != nil {
		return nil, false, err
	}
	return r.Value, r.Found, nil
}

func (s hostStateStore) Put(ctx context.Context, key string, raw []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.client.KVSet(ctx, &pluginv1.KVSetRequest{Namespace: "basispoints-tools-v1", Key: key, Value: raw, TtlSeconds: bridge.StateTTLSeconds})
	return err
}
