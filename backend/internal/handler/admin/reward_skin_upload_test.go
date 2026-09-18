package admin

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type skinUploadRepository struct {
	service.RewardRepository
	skin    service.RewardSkin
	content []byte
	calls   int
}

func (r *skinUploadRepository) CreateSkin(_ context.Context, skin service.RewardSkin, content []byte, _ *int64) (*service.RewardSkin, error) {
	r.calls++
	r.skin, r.content = skin, append([]byte(nil), content...)
	skin.ID, skin.Status = 42, "active"
	return &skin, nil
}

func TestRewardSkinUploadImageDecoderRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	webp, err := os.ReadFile("../../testutil/testdata/security-images/skin-lossless.webp")
	require.NoError(t, err)
	var pngImage, jpegImage bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1320, 500))
	require.NoError(t, png.Encode(&pngImage, img))
	require.NoError(t, jpeg.Encode(&jpegImage, img, nil))
	for _, tc := range []struct {
		name, mime string
		data       []byte
		status     int
	}{
		{"webp", "image/webp", webp, http.StatusCreated},
		{"png", "image/png", pngImage.Bytes(), http.StatusCreated},
		{"jpeg", "image/jpeg", jpegImage.Bytes(), http.StatusCreated},
		{"truncated_webp", "", webp[:16], http.StatusBadRequest},
		{"invalid", "", []byte("not an image"), http.StatusBadRequest},
		{"empty", "", nil, http.StatusBadRequest},
		{"over_limit", "", make([]byte, 1024*1024+1), http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &skinUploadRepository{}
			h := NewRewardHandler(service.NewRewardService(repo, nil, nil))
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("name", "Decoder regression"))
			file, err := writer.CreateFormFile("file", tc.name)
			require.NoError(t, err)
			_, err = file.Write(tc.data)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/rewards/skins", &body)
			ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
			require.NotPanics(t, func() { h.UploadSkin(ctx) })
			require.Equal(t, tc.status, recorder.Code, "%s", recorder.Body.String())
			if tc.status != http.StatusCreated {
				require.Zero(t, repo.calls, "invalid input must not reach persistence")
				return
			}
			require.Equal(t, 1, repo.calls)
			require.Equal(t, tc.mime, repo.skin.MIMEType)
			require.Equal(t, 1320, repo.skin.Width)
			require.Equal(t, 500, repo.skin.Height)
			require.Equal(t, tc.data, repo.content)
		})
	}
}
