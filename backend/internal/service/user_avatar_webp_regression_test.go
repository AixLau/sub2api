//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	// The server registers WebP through admin/reward_handler.go. Register the
	// same decoder when testing the service package by itself.
	_ "golang.org/x/image/webp"
)

func TestUpdateProfileWebPDecoderRegression(t *testing.T) {
	valid, err := os.ReadFile("../testutil/testdata/security-images/avatar-lossless.webp")
	require.NoError(t, err)
	require.Greater(t, len(valid), targetAvatarBytes, "must exercise image.Decode, not the small-avatar pass-through")
	// A VP8X canvas of 1x1 disagrees with the actual 128x128 VP8L frame.
	mismatch := append([]byte(nil), valid[:12]...)
	mismatch = append(mismatch, []byte{'V', 'P', '8', 'X', 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	mismatch = append(mismatch, valid[12:]...)
	binary.LittleEndian.PutUint32(mismatch[4:8], uint32(len(mismatch)-8))
	for _, tc := range []struct {
		name    string
		data    []byte
		invalid bool
	}{
		{name: "lossless_webp", data: valid},
		{name: "canvas_mismatch", data: mismatch, invalid: true},
		{name: "truncated_webp", data: valid[:targetAvatarBytes+1], invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockUserRepo{getByIDUser: &User{ID: 42, Email: "webp@example.test"}}
			svc := NewUserService(repo, nil, nil, nil)
			dataURL := "data:image/webp;base64," + base64.StdEncoding.EncodeToString(tc.data)
			var updated *User
			var updateErr error
			require.NotPanics(t, func() {
				updated, updateErr = svc.UpdateProfile(context.Background(), 42, UpdateProfileRequest{AvatarURL: &dataURL})
			})
			if tc.invalid {
				require.ErrorIs(t, updateErr, ErrAvatarInvalid)
				require.Empty(t, repo.upsertAvatarArgs)
				return
			}
			require.NoError(t, updateErr)
			require.Len(t, repo.upsertAvatarArgs, 1)
			require.Equal(t, "image/jpeg", updated.AvatarMIME)
			require.LessOrEqual(t, updated.AvatarByteSize, targetAvatarBytes)
			_, encoded, ok := bytes.Cut([]byte(updated.AvatarURL), []byte(","))
			require.True(t, ok)
			converted, err := base64.StdEncoding.DecodeString(string(encoded))
			require.NoError(t, err)
			cfg, format, err := image.DecodeConfig(bytes.NewReader(converted))
			require.NoError(t, err)
			require.Equal(t, "jpeg", format)
			require.Greater(t, cfg.Width, 0)
			require.Greater(t, cfg.Height, 0)
		})
	}
}
