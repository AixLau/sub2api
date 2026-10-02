package pluginv1

import (
	"encoding/json"
	"errors"
	"net/http"

	"google.golang.org/grpc/metadata"
)

// AccountClientHeadersMetadataKey is private host/plugin metadata, never an
// inbound or outbound HTTP header. It carries only approved client identity
// fields from the selected account, separately from native transport headers.
const AccountClientHeadersMetadataKey = "sub2api-account-client-headers-bin"

const maxAccountClientHeadersJSONBytes = 256 * 1024

func AccountClientHeadersMetadata(headers http.Header) (metadata.MD, error) {
	data, err := json.Marshal(headers)
	if err != nil || len(data) > maxAccountClientHeadersJSONBytes {
		return nil, errors.New("invalid account client header snapshot")
	}
	return metadata.Pairs(AccountClientHeadersMetadataKey, string(data)), nil
}

func AccountClientHeaders(md metadata.MD) (http.Header, error) {
	values := md.Get(AccountClientHeadersMetadataKey)
	if len(values) == 0 {
		return make(http.Header), nil
	}
	if len(values) != 1 || len(values[0]) > maxAccountClientHeadersJSONBytes {
		return nil, errors.New("invalid account client header snapshot")
	}
	var headers http.Header
	if json.Unmarshal([]byte(values[0]), &headers) != nil {
		return nil, errors.New("invalid account client header snapshot")
	}
	return headers, nil
}
