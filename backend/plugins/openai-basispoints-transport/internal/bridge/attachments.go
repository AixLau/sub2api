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

// RewriteInlineImages replaces data-URL input_image parts in user messages with
// file_id references produced by upload. Existing file ids, remote URLs,
// assistant messages and tool outputs are left untouched, and the body is
// returned unchanged when no inline image is present. cacheNamespace isolates
// the upload cache per endpoint and credential.
func RewriteInlineImages(ctx context.Context, body []byte, cacheNamespace string, upload UploadFunc) ([]byte, error) {
	root, err := parseObject(body)
	if err != nil {
		return nil, err
	}
	var input []json.RawMessage
	if len(root["input"]) == 0 || json.Unmarshal(root["input"], &input) != nil {
		return body, nil
	}
	changed := false
	for i, raw := range input {
		item, err := parseObject(raw)
		if err != nil {
			continue
		}
		if stringValue(item["role"]) != "user" {
			continue
		}
		if typ := stringValue(item["type"]); typ != "" && typ != "message" {
			continue
		}
		var parts []json.RawMessage
		if len(item["content"]) == 0 || json.Unmarshal(item["content"], &parts) != nil {
			continue
		}
		updated := append([]json.RawMessage(nil), parts...)
		partsChanged := false
		for j, partRaw := range parts {
			part, err := parseObject(partRaw)
			if err != nil {
				continue
			}
			if stringValue(part["type"]) != "input_image" {
				continue
			}
			imageURL := stringValue(part["image_url"])
			if imageURL == "" {
				// Chat Completions style {"url": "data:..."} parts.
				if nested, err := parseObject(part["image_url"]); err == nil {
					imageURL = stringValue(nested["url"])
				}
			}
			if len(imageURL) < 5 || !strings.EqualFold(imageURL[:5], "data:") {
				continue
			}
			if stringValue(part["file_id"]) != "" {
				return nil, errors.New("input_image 不能同时包含 image_url 和 file_id")
			}
			mediaType, data, err := decodeInlineImage(imageURL)
			if err != nil {
				return nil, err
			}
			key := attachmentCacheKey(cacheNamespace, mediaType, data)
			fileID, err := attachmentUploads.getOrUpload(key, func() (string, error) {
				return upload(ctx, mediaType, data)
			})
			if err != nil {
				return nil, err
			}
			replaced := object{}
			for name, value := range part {
				replaced[name] = value
			}
			delete(replaced, "image_url")
			replaced["file_id"] = encoded(fileID)
			if _, exists := replaced["detail"]; !exists {
				replaced["detail"] = encoded("auto")
			}
			updated[j] = encoded(replaced)
			partsChanged = true
		}
		if partsChanged {
			item["content"] = encoded(updated)
			input[i] = encoded(item)
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	root["input"] = encoded(input)
	return json.Marshal(root)
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

// StripEncryptedContent removes replay-side encrypted payloads at any depth
// (reasoning items issued under another channel's keys, and encrypted function
// outputs nested inside content parts). Ciphertexts are channel-scoped:
// replaying them to a different backend fails the whole response with
// invalid_encrypted_content.
func StripEncryptedContent(body []byte) ([]byte, error) {
	root, err := parseObject(body)
	if err != nil {
		return nil, err
	}
	var input []json.RawMessage
	if len(root["input"]) == 0 || json.Unmarshal(root["input"], &input) != nil {
		return body, nil
	}
	changed := false
	kept := make([]json.RawMessage, 0, len(input))
	for _, raw := range input {
		item, err := parseObject(raw)
		if err != nil {
			kept = append(kept, raw)
			continue
		}
		if stringValue(item["type"]) == "reasoning" {
			// A reasoning item exists only to replay its (channel-scoped)
			// ciphertext; without it there is nothing worth keeping.
			if hasEncryptedAnywhere(raw) {
				changed = true
				continue
			}
			kept = append(kept, raw)
			continue
		}
		cleaned, didChange := stripEncryptedKeys(raw)
		if didChange {
			changed = true
		}
		kept = append(kept, cleaned)
	}
	if !changed {
		return body, nil
	}
	root["input"] = encoded(kept)
	return json.Marshal(root)
}

// stripEncryptedKeys recursively removes every encrypted_content key from a
// JSON value and reports whether anything changed.
func stripEncryptedKeys(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := json.RawMessage(bytes.TrimSpace(raw))
	if len(trimmed) == 0 {
		return raw, false
	}
	switch trimmed[0] {
	case '{':
		obj, err := parseObject(trimmed)
		if err != nil {
			return raw, false
		}
		changed := false
		if _, ok := obj["encrypted_content"]; ok {
			delete(obj, "encrypted_content")
			changed = true
		}
		for key, value := range obj {
			cleaned, didChange := stripEncryptedKeys(value)
			if didChange {
				obj[key] = cleaned
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		return encoded(obj), true
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(trimmed, &items) != nil {
			return raw, false
		}
		changed := false
		for i, item := range items {
			cleaned, didChange := stripEncryptedKeys(item)
			if didChange {
				items[i] = cleaned
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		return encoded(items), true
	}
	return raw, false
}

func hasEncryptedAnywhere(raw json.RawMessage) bool {
	trimmed := json.RawMessage(bytes.TrimSpace(raw))
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '{':
		obj, err := parseObject(trimmed)
		if err != nil {
			return false
		}
		if _, ok := obj["encrypted_content"]; ok {
			return true
		}
		for _, value := range obj {
			if hasEncryptedAnywhere(value) {
				return true
			}
		}
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(trimmed, &items) != nil {
			return false
		}
		for _, item := range items {
			if hasEncryptedAnywhere(item) {
				return true
			}
		}
	}
	return false
}
