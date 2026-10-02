package pluginv1

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func TestAccountClientHeadersMetadata(t *testing.T) {
	original := http.Header{"User-Agent": {"captured-browser"}}
	md, err := AccountClientHeadersMetadata(original)
	require.NoError(t, err)
	got, err := AccountClientHeaders(md)
	require.NoError(t, err)
	require.Equal(t, original, got)
	got.Set("User-Agent", "changed")
	require.Equal(t, "captured-browser", original.Get("User-Agent"))
	got, err = AccountClientHeaders(nil)
	require.NoError(t, err)
	require.Empty(t, got)
	for _, values := range [][]string{{"not-json"}, {"{}", "{}"}, {`{"User-Agent":"wrong-type"}`}} {
		_, err := AccountClientHeaders(metadata.MD{AccountClientHeadersMetadataKey: values})
		require.Error(t, err)
	}
}
