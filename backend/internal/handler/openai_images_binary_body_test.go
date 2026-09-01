package handler

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestReadOpenAIHTTPPreForwardRequest_PreservesMultipartImageBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	imageBytes := pngWithQuotedControlBytes(t)
	_, err := png.Decode(bytes.NewReader(imageBytes))
	require.NoError(t, err, "fixture must be a valid PNG before entering the gateway")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "preserve the source image"))
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="source.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(imageBytes)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.Request = req
	handler := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}}

	_, _, _, _, parsed, ok := handler.readOpenAIHTTPPreForwardRequest(
		c,
		zap.NewNop(),
		service.ContentModerationProtocolOpenAIImages,
	)

	require.True(t, ok)
	require.NotNil(t, parsed)
	require.Len(t, parsed.Uploads, 1)
	require.Equal(t, imageBytes, parsed.Uploads[0].Data)
	_, err = png.Decode(bytes.NewReader(parsed.Uploads[0].Data))
	require.NoError(t, err, "moderation pre-read must not corrupt uploaded image bytes")
}

func pngWithQuotedControlBytes(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	source := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 32, G: 96, B: 160, A: 255})
	require.NoError(t, png.Encode(&encoded, source))

	pngBytes := encoded.Bytes()
	const signatureSize = 8
	require.Greater(t, len(pngBytes), signatureSize+12)
	ihdrDataLength := int(binary.BigEndian.Uint32(pngBytes[signatureSize : signatureSize+4]))
	insertAt := signatureSize + 12 + ihdrDataLength
	require.Less(t, insertAt, len(pngBytes))

	chunkType := []byte("ruSt")
	chunkData := []byte{'"', 0x01, '"', 0x02}
	var chunk bytes.Buffer
	require.NoError(t, binary.Write(&chunk, binary.BigEndian, uint32(len(chunkData))))
	_, err := chunk.Write(chunkType)
	require.NoError(t, err)
	_, err = chunk.Write(chunkData)
	require.NoError(t, err)
	checksumInput := append(append([]byte(nil), chunkType...), chunkData...)
	require.NoError(t, binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(checksumInput)))

	result := make([]byte, 0, len(pngBytes)+chunk.Len())
	result = append(result, pngBytes[:insertAt]...)
	result = append(result, chunk.Bytes()...)
	result = append(result, pngBytes[insertAt:]...)
	return result
}
