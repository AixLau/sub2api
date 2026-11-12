package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 用户群二维码在后台以 data URI（base64）保存，单张可达上百 KB。
// 公开设置会被注入到每个 HTML 页面（no-store），直接内嵌会让每次打开页面
// 都重复下载这些图片。这里把 data URI 换成带内容版本号的同源图片地址，
// 图片本身由独立接口按长缓存返回，前端只在打开客服弹窗时才加载。

const (
	SupportQRCodeKindQQ     = "qq"
	SupportQRCodeKindWeChat = "wechat"

	supportQRCodePublicPathPrefix = "/api/v1/settings/public/support-qr/"
)

var ErrSupportQRCodeNotFound = infraerrors.NotFound("SUPPORT_QR_CODE_NOT_FOUND", "support qr code not found")

// SupportQRCodeImage 是解码后的二维码图片。
type SupportQRCodeImage struct {
	ContentType string
	Data        []byte
	Version     string
}

func supportQRCodeSettingKey(kind string) (string, bool) {
	switch kind {
	case SupportQRCodeKindQQ:
		return SettingKeySupportQQGroupQRCode, true
	case SupportQRCodeKindWeChat:
		return SettingKeySupportWeChatGroupQRCode, true
	default:
		return "", false
	}
}

// supportQRCodeVersion 用内容哈希做版本号：图片不变地址不变，后台换图后地址随之变化。
func supportQRCodeVersion(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:6])
}

// decodeImageDataURI 解析 data:image/<type>;base64,<payload>。不是可解码的图片 data URI 时返回 false。
func decodeImageDataURI(raw string) (contentType string, data []byte, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < len("data:image/") || !strings.EqualFold(trimmed[:len("data:image/")], "data:image/") {
		return "", nil, false
	}
	comma := strings.IndexByte(trimmed, ',')
	if comma == -1 {
		return "", nil, false
	}
	meta := trimmed[len("data:"):comma]
	parts := strings.Split(meta, ";")
	isBase64 := false
	for _, p := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(p), "base64") {
			isBase64 = true
		}
	}
	if !isBase64 {
		return "", nil, false
	}
	contentType = strings.ToLower(strings.TrimSpace(parts[0]))
	switch contentType {
	case "image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif", "image/svg+xml":
	default:
		return "", nil, false
	}
	if contentType == "image/jpg" {
		contentType = "image/jpeg"
	}
	payload := strings.TrimSpace(trimmed[comma+1:])
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(payload, "="))
		if err != nil {
			return "", nil, false
		}
	}
	if len(decoded) == 0 {
		return "", nil, false
	}
	return contentType, decoded, true
}

// publicSupportQRCodeRef 把后台保存的二维码值转换成适合公开下发的引用。
// data URI 换成同源图片地址；http(s) 地址、空值等原样返回。
func publicSupportQRCodeRef(kind, raw string) string {
	if _, _, ok := decodeImageDataURI(raw); !ok {
		return raw
	}
	return supportQRCodePublicPathPrefix + kind + "?v=" + supportQRCodeVersion(strings.TrimSpace(raw))
}

// GetSupportQRCodeImage 读取并解码指定的用户群二维码。
func (s *SettingService) GetSupportQRCodeImage(ctx context.Context, kind string) (*SupportQRCodeImage, error) {
	key, ok := supportQRCodeSettingKey(kind)
	if !ok {
		return nil, ErrSupportQRCodeNotFound
	}
	vals, err := s.settingRepo.GetMultiple(ctx, []string{key})
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(vals[key])
	contentType, data, ok := decodeImageDataURI(raw)
	if !ok {
		return nil, ErrSupportQRCodeNotFound
	}
	return &SupportQRCodeImage{
		ContentType: contentType,
		Data:        data,
		Version:     supportQRCodeVersion(raw),
	}, nil
}
