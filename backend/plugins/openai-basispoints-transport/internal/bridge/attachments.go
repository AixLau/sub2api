package bridge

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Basis Points rejects inline data-URL images in user messages. Images must be
// uploaded to the attachments endpoint next to /responses and referenced by
// file_id, which is the official Excel add-in contract.

// UploadFunc uploads one image and returns the upstream file id.
type UploadFunc func(ctx context.Context, mediaType string, data []byte) (string, error)

// maxAttachmentCacheEntries bounds the in-process upload cache. Only digests
// and file ids are kept; image bytes and credentials are never stored.
const maxAttachmentCacheEntries = 512

type cachedAttachment struct {
	key    [sha256.Size]byte
	fileID string
}

type pendingAttachment struct {
	done   chan struct{}
	fileID string
	err    error
}

type attachmentCache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]*list.Element
	order   list.List
	pending map[[sha256.Size]byte]*pendingAttachment
}

func (c *attachmentCache) getOrUpload(key [sha256.Size]byte, upload func() (string, error)) (string, error) {
	c.mu.Lock()
	if entry := c.entries[key]; entry != nil {
		c.order.MoveToFront(entry)
		fileID := entry.Value.(cachedAttachment).fileID
		c.mu.Unlock()
		return fileID, nil
	}
	if pending := c.pending[key]; pending != nil {
		c.mu.Unlock()
		<-pending.done
		return pending.fileID, pending.err
	}
	if c.pending == nil {
		c.pending = make(map[[sha256.Size]byte]*pendingAttachment)
	}
	pending := &pendingAttachment{done: make(chan struct{})}
	c.pending[key] = pending
	c.mu.Unlock()

	fileID, err := upload()
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, key)
	if err == nil {
		if c.entries == nil {
			c.entries = make(map[[sha256.Size]byte]*list.Element)
		}
		c.entries[key] = c.order.PushFront(cachedAttachment{key: key, fileID: fileID})
		if c.order.Len() > maxAttachmentCacheEntries {
			oldest := c.order.Back()
			delete(c.entries, oldest.Value.(cachedAttachment).key)
			c.order.Remove(oldest)
		}
	}
	pending.fileID, pending.err = fileID, err
	close(pending.done)
	return fileID, err
}

var attachmentUploads attachmentCache

// imageFileExtensions maps canonical media types to the file extensions the
// attachments endpoint accepts. mime.ExtensionsByType is deliberately not used:
// it returns nothing for image/jpg and image/x-png, and .jpe for image/jpeg.
var imageFileExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

var imageTypeAliases = map[string]string{
	"image/jpg":    "image/jpeg",
	"image/pjpeg":  "image/jpeg",
	"image/x-png":  "image/png",
	"image/apng":   "image/png",
	"image/x-webp": "image/webp",
}

// CanonicalImageType normalizes an image media type to the canonical form the
// attachments endpoint understands, and reports the file extension to use.
func CanonicalImageType(mediaType string) (string, string, bool) {
	canonical, ok := imageTypeAliases[mediaType]
	if !ok {
		canonical = mediaType
	}
	extension, ok := imageFileExtensions[canonical]
	return canonical, extension, ok
}

// RewriteInlineImages uploads inline input_image parts throughout input,
// including nested tool results. Remote URLs and existing file IDs are kept
// unchanged. cacheNamespace isolates uploads per endpoint and credential.
func RewriteInlineImages(ctx context.Context, body []byte, cacheNamespace string, upload UploadFunc) ([]byte, error) {
	root, err := parseObject(body)
	if err != nil {
		return nil, err
	}
	input, changed, err := rewriteImageParts(ctx, root["input"], cacheNamespace, upload)
	if err != nil {
		return nil, err
	}
	if !changed {
		return body, nil
	}
	root["input"] = input
	return json.Marshal(root)
}

func inlineImageURL(part object) string {
	if value := stringValue(part["image_url"]); value != "" {
		return value
	}
	if nested, err := parseObject(part["image_url"]); err == nil {
		return stringValue(nested["url"])
	}
	return ""
}

func isInlineImageURL(value string) bool {
	return len(value) >= 5 && strings.EqualFold(value[:5], "data:")
}

func rewriteImageParts(ctx context.Context, raw json.RawMessage, namespace string, upload UploadFunc) (json.RawMessage, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return raw, false, nil
	}
	switch raw[0] {
	case '{':
		item, err := parseObject(raw)
		if err != nil {
			return nil, false, err
		}
		if stringValue(item["type"]) == "input_image" {
			imageURL := inlineImageURL(item)
			if !isInlineImageURL(imageURL) {
				return raw, false, nil
			}
			if stringValue(item["file_id"]) != "" {
				return nil, false, errors.New("input_image 不能同时包含 image_url 和 file_id")
			}
			mediaType, data, err := decodeInlineImage(imageURL)
			if err != nil {
				return nil, false, err
			}
			key := attachmentCacheKey(namespace, mediaType, data)
			fileID, err := attachmentUploads.getOrUpload(key, func() (string, error) { return upload(ctx, mediaType, data) })
			if err != nil {
				return nil, false, err
			}
			delete(item, "image_url")
			item["file_id"] = encoded(fileID)
			if _, exists := item["detail"]; !exists {
				item["detail"] = encoded("auto")
			}
			return encoded(item), true, nil
		}
		changed := false
		// Sort keys so upload order is deterministic across retries.
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			updated, childChanged, err := rewriteImageParts(ctx, item[key], namespace, upload)
			if err != nil {
				return nil, false, err
			}
			if childChanged {
				item[key] = updated
				changed = true
			}
		}
		if changed {
			return encoded(item), true, nil
		}
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, false, err
		}
		changed := false
		for i, item := range items {
			updated, childChanged, err := rewriteImageParts(ctx, item, namespace, upload)
			if err != nil {
				return nil, false, err
			}
			if childChanged {
				items[i] = updated
				changed = true
			}
		}
		if changed {
			return encoded(items), true, nil
		}
	}
	return raw, false, nil
}

func attachmentCacheKey(namespace, mediaType string, data []byte) [sha256.Size]byte {
	hash := sha256.New()
	for _, part := range []string{namespace, mediaType} {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	hash.Write(data)
	var key [sha256.Size]byte
	copy(key[:], hash.Sum(nil))
	return key
}

func decodeInlineImage(dataURL string) (string, []byte, error) {
	metadata, payload, found := strings.Cut(dataURL[5:], ",")
	if !found {
		return "", nil, errors.New("input_image data URL 缺少数据分隔符")
	}
	isBase64 := strings.HasSuffix(strings.ToLower(metadata), ";base64")
	if isBase64 {
		metadata = metadata[:len(metadata)-len(";base64")]
	}
	mediaType, _, err := mime.ParseMediaType(metadata)
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return "", nil, errors.New("input_image data URL 必须声明 image/* 媒体类型")
	}
	canonical, _, supported := CanonicalImageType(mediaType)
	if !supported {
		return "", nil, errors.New("input_image 图片格式 " + mediaType + " 不受支持；仅支持 JPEG/PNG/GIF/WebP")
	}
	mediaType = canonical
	decoded, err := url.PathUnescape(payload)
	if err != nil {
		return "", nil, errors.New("input_image data URL 百分号编码无效")
	}
	var data []byte
	if isBase64 {
		data, err = base64.StdEncoding.DecodeString(decoded)
	} else {
		data = []byte(decoded)
	}
	if err != nil || len(data) == 0 {
		return "", nil, errors.New("input_image data URL 图片数据为空或无效")
	}
	return mediaType, data, nil
}
