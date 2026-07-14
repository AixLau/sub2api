package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseUsageRequestType(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		input   string
		want    RequestType
		wantErr bool
	}

	cases := []testCase{
		{name: "unknown", input: "unknown", want: RequestTypeUnknown},
		{name: "sync", input: "sync", want: RequestTypeSync},
		{name: "stream", input: "stream", want: RequestTypeStream},
		{name: "ws_v2", input: "ws_v2", want: RequestTypeWSV2},
		{name: "case_insensitive", input: "WS_V2", want: RequestTypeWSV2},
		{name: "trim_spaces", input: "  stream  ", want: RequestTypeStream},
		{name: "invalid", input: "xxx", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseUsageRequestType(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRequestTypeNormalizeAndString(t *testing.T) {
	t.Parallel()

	require.Equal(t, RequestTypeUnknown, RequestType(99).Normalize())
	require.Equal(t, "unknown", RequestType(99).String())
	require.Equal(t, "sync", RequestTypeSync.String())
	require.Equal(t, "stream", RequestTypeStream.String())
	require.Equal(t, "ws_v2", RequestTypeWSV2.String())
}

func TestRequestTypeFromLegacy(t *testing.T) {
	t.Parallel()

	require.Equal(t, RequestTypeWSV2, RequestTypeFromLegacy(false, true))
	require.Equal(t, RequestTypeStream, RequestTypeFromLegacy(true, false))
	require.Equal(t, RequestTypeSync, RequestTypeFromLegacy(false, false))
}

func TestApplyLegacyRequestFields(t *testing.T) {
	t.Parallel()

	stream, ws := ApplyLegacyRequestFields(RequestTypeSync, true, true)
	require.False(t, stream)
	require.False(t, ws)

	stream, ws = ApplyLegacyRequestFields(RequestTypeStream, false, true)
	require.True(t, stream)
	require.False(t, ws)

	stream, ws = ApplyLegacyRequestFields(RequestTypeWSV2, false, false)
	require.True(t, stream)
	require.True(t, ws)

	stream, ws = ApplyLegacyRequestFields(RequestTypeUnknown, true, false)
	require.True(t, stream)
	require.False(t, ws)
}

func TestUsageLogSyncRequestTypeAndLegacyFields(t *testing.T) {
	t.Parallel()

	log := &UsageLog{RequestType: RequestTypeWSV2, Stream: false, OpenAIWSMode: false}
	log.SyncRequestTypeAndLegacyFields()

	require.Equal(t, RequestTypeWSV2, log.RequestType)
	require.True(t, log.Stream)
	require.True(t, log.OpenAIWSMode)
}

func TestUsageLogEffectiveRequestTypeFallback(t *testing.T) {
	t.Parallel()

	log := &UsageLog{RequestType: RequestTypeUnknown, Stream: true, OpenAIWSMode: true}
	require.Equal(t, RequestTypeWSV2, log.EffectiveRequestType())
}

func TestUsageLogEffectiveRequestTypeNilReceiver(t *testing.T) {
	t.Parallel()

	var log *UsageLog
	require.Equal(t, RequestTypeUnknown, log.EffectiveRequestType())
}

func TestUsageLogSyncRequestTypeAndLegacyFieldsNilReceiver(t *testing.T) {
	t.Parallel()

	var log *UsageLog
	log.SyncRequestTypeAndLegacyFields()
}

func TestUsageLogValidateActors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		log     *UsageLog
		wantErr string
	}{
		{
			name: "gateway actors are valid",
			log:  &UsageLog{Source: UsageSourceGateway, UserID: 1, APIKeyID: 2, AccountID: 3},
		},
		{
			name:    "gateway requires user",
			log:     &UsageLog{Source: UsageSourceGateway, APIKeyID: 2, AccountID: 3},
			wantErr: "user_id",
		},
		{
			name:    "gateway requires api key",
			log:     &UsageLog{Source: UsageSourceGateway, UserID: 1, AccountID: 3},
			wantErr: "api_key_id",
		},
		{
			name: "account test is actorless",
			log:  &UsageLog{Source: UsageSourceAccountTest, AccountID: 7},
		},
		{
			name: "content moderation is actorless",
			log:  &UsageLog{Source: UsageSourceContentModeration, AccountID: 8},
		},
		{
			name:    "account test rejects user",
			log:     &UsageLog{Source: UsageSourceAccountTest, UserID: 1, AccountID: 7},
			wantErr: "account_test",
		},
		{
			name:    "account test rejects api key",
			log:     &UsageLog{Source: UsageSourceAccountTest, APIKeyID: 2, AccountID: 7},
			wantErr: "account_test",
		},
		{
			name:    "content moderation rejects user",
			log:     &UsageLog{Source: UsageSourceContentModeration, UserID: 1, AccountID: 8},
			wantErr: "content_moderation",
		},
		{
			name:    "account test requires account",
			log:     &UsageLog{Source: UsageSourceAccountTest},
			wantErr: "account_id",
		},
		{
			name:    "nil usage log",
			wantErr: "usage log",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.log.ValidateActors()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestUsageSourceFailedUpstreamIsGatewayActorUsage(t *testing.T) {
	source := UsageSourceFailedUpstream
	require.Equal(t, UsageSourceFailedUpstream, source.Normalize())
	require.False(t, source.IsPlatformOperation())
	require.NoError(t, (&UsageLog{
		Source:    source,
		UserID:    1,
		APIKeyID:  2,
		AccountID: 3,
	}).ValidateActors())
}
