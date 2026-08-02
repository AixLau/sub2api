//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageBillingModelForSource(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		requested     string
		channelMapped string
		forwarded     string
		upstream      string
		explicit      string
		want          string
	}{
		{
			name:          "requested keeps client model",
			source:        BillingModelSourceRequested,
			requested:     "client-model",
			channelMapped: "channel-model",
			upstream:      "provider-model",
			want:          "client-model",
		},
		{
			name:          "channel mapping uses explicit channel target",
			source:        BillingModelSourceChannelMapped,
			requested:     "client-model",
			channelMapped: "channel-model",
			upstream:      "provider-model",
			want:          "channel-model",
		},
		{
			name:          "channel mode without channel mapping uses later mapped target",
			source:        BillingModelSourceChannelMapped,
			requested:     "client-model",
			channelMapped: "client-model",
			forwarded:     "client-model",
			upstream:      "provider-model",
			explicit:      "account-model",
			want:          "provider-model",
		},
		{
			name:          "upstream uses final provider target",
			source:        BillingModelSourceUpstream,
			requested:     "client-model",
			channelMapped: "channel-model",
			forwarded:     "channel-model",
			upstream:      "provider-model",
			explicit:      "account-model",
			want:          "provider-model",
		},
		{
			name:      "legacy source preserves explicit billing model",
			forwarded: "client-model",
			upstream:  "provider-model",
			explicit:  "legacy-billing-model",
			want:      "legacy-billing-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, usageBillingModelForSource(
				tt.source,
				tt.requested,
				tt.channelMapped,
				tt.forwarded,
				tt.upstream,
				tt.explicit,
			))
		})
	}
}
