package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func imageDataURL() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
}

func rewriteBody(t *testing.T, namespace string, upload UploadFunc, input ...any) ([]byte, object) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"input": input})
	require.NoError(t, err)
	out, err := RewriteInlineImages(context.Background(), raw, namespace, upload)
	require.NoError(t, err)
	root, err := parseObject(out)
	require.NoError(t, err)
	return out, root
}

func TestInlineImageBecomesFileReference(t *testing.T) {
	uploads := 0
	out, root := rewriteBody(t, "ns-inline", func(_ context.Context, mediaType string, data []byte) (string, error) {
		uploads++
		require.Equal(t, "image/png", mediaType)
		require.Equal(t, []byte("png-bytes"), data)
		return "file-uploaded", nil
	},
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_image", "image_url": imageDataURL(), "detail": "high"},
			map[string]any{"type": "input_image", "image_url": imageDataURL()},
		}},
	)
	require.Equal(t, 1, uploads, "identical images must upload once")
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &items))
	require.Len(t, items, 1)
	item, _ := parseObject(items[0])
	var parts []json.RawMessage
	require.NoError(t, json.Unmarshal(item["content"], &parts))
	require.JSONEq(t, `{"type":"input_image","file_id":"file-uploaded","detail":"high"}`, string(parts[0]))
	require.JSONEq(t, `{"type":"input_image","file_id":"file-uploaded","detail":"auto"}`, string(parts[1]))

	// A retry of the same conversation reuses the cached file id.
	retried, err := RewriteInlineImages(context.Background(), out, "ns-inline", func(context.Context, string, []byte) (string, error) {
		uploads++
		return "file-other", nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, uploads)
	require.JSONEq(t, string(out), string(retried))
}

func TestImageUploadPreservesOtherInputKinds(t *testing.T) {
	source := map[string]any{"input": []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "input_image", "file_id": "file-existing", "detail": "high"},
			map[string]any{"type": "input_image", "image_url": "https://example.test/image.png", "detail": "auto"},
		}},
		map[string]any{"type": "function_call_output", "call_id": "call_1", "output": []any{map[string]any{"type": "input_image", "file_id": "file-existing"}}},
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "input_image", "file_id": "file-existing"}}},
	}}
	before, err := json.Marshal(source)
	require.NoError(t, err)
	out, err := RewriteInlineImages(context.Background(), before, "ns-preserve", func(context.Context, string, []byte) (string, error) {
		t.Fatal("unexpected upload for preserved input kinds")
		return "", errors.New("unexpected")
	})
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(out))
}

func TestInvalidInlineImagesFailBeforeUpload(t *testing.T) {
	upload := func(context.Context, string, []byte) (string, error) {
		t.Fatal("invalid images must not be uploaded")
		return "", errors.New("unexpected")
	}
	for _, part := range []map[string]any{
		{"type": "input_image", "image_url": "data:image/png;base64"},
		{"type": "input_image", "image_url": "data:text/plain;base64,aGVsbG8="},
		{"type": "input_image", "image_url": "data:image/png;base64,"},
		{"type": "input_image", "image_url": "data:image/heic;base64,aGVsbG8="},
		{"type": "input_image", "image_url": imageDataURL(), "file_id": "file-existing"},
	} {
		raw, err := json.Marshal(map[string]any{"input": []any{
			map[string]any{"role": "user", "content": []any{part}},
		}})
		require.NoError(t, err)
		_, err = RewriteInlineImages(context.Background(), raw, "ns-invalid", upload)
		require.Error(t, err, part)
	}
}

func TestInlineImagesInNestedToolResults(t *testing.T) {
	input := []any{
		map[string]any{"type": "function_call_output", "call_id": "call_1", "output": []any{
			map[string]any{"type": "input_text", "text": "screenshot"},
			map[string]any{"type": "input_image", "image_url": imageDataURL(), "detail": "high"},
		}},
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "input_image", "image_url": map[string]any{"url": imageDataURL()}},
		}},
	}
	raw := mustJSON(t, map[string]any{"input": input})
	uploads := 0
	upload := func(context.Context, string, []byte) (string, error) {
		uploads++
		return "file-nested", nil
	}
	out, err := RewriteInlineImages(context.Background(), raw, "ns-nested", upload)
	require.NoError(t, err)
	require.Equal(t, 1, uploads)
	require.NotContains(t, string(out), "data:image")
	require.NotContains(t, string(out), "image_url")
	require.Equal(t, 2, strings.Count(string(out), "file-nested"))
	require.Contains(t, string(out), `"call_id":"call_1"`)
	require.Contains(t, string(out), `"text":"screenshot"`)
	require.Contains(t, string(out), `"detail":"high"`)
	// Replaying the original data URLs must reuse the IDs, too.
	again, err := RewriteInlineImages(context.Background(), raw, "ns-nested", upload)
	require.NoError(t, err)
	require.JSONEq(t, string(out), string(again))
	require.Equal(t, 1, uploads)
}

func TestImageTypesAreCanonicalizedAndNamed(t *testing.T) {
	for requested, want := range map[string]struct{ mediaType, filename string }{
		"image/jpeg":  {"image/jpeg", "image.jpg"},
		"image/jpg":   {"image/jpeg", "image.jpg"},
		"image/pjpeg": {"image/jpeg", "image.jpg"},
		"image/png":   {"image/png", "image.png"},
		"image/x-png": {"image/png", "image.png"},
		"image/gif":   {"image/gif", "image.gif"},
		"image/webp":  {"image/webp", "image.webp"},
	} {
		canonical, extension, ok := CanonicalImageType(requested)
		require.True(t, ok, requested)
		require.Equal(t, want.mediaType, canonical, requested)
		require.Equal(t, want.filename, "image"+extension, requested)
	}
	for _, unsupported := range []string{"image/heic", "image/avif", "image/tiff", "image/bmp", "image/svg+xml"} {
		_, _, ok := CanonicalImageType(unsupported)
		require.False(t, ok, unsupported)
	}
	// A non-canonical type is canonicalized before the upload callback runs.
	var gotTypes []string
	out, root := rewriteBody(t, "ns-canonical", func(_ context.Context, mediaType string, _ []byte) (string, error) {
		gotTypes = append(gotTypes, mediaType)
		if mediaType == "image/jpeg" {
			return "file-jpg", nil
		}
		return "file-uploaded", nil
	},
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_image", "image_url": "data:image/jpg;base64," + base64.StdEncoding.EncodeToString([]byte("jpg-bytes"))},
			map[string]any{"type": "input_image", "image_url": map[string]any{"url": imageDataURL()}},
		}},
	)
	require.Equal(t, []string{"image/jpeg", "image/png"}, gotTypes)
	_ = out
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &items))
	item, _ := parseObject(items[0])
	var parts []json.RawMessage
	require.NoError(t, json.Unmarshal(item["content"], &parts))
	require.JSONEq(t, `{"type":"input_image","file_id":"file-jpg","detail":"auto"}`, string(parts[0]))
	require.JSONEq(t, `{"type":"input_image","file_id":"file-uploaded","detail":"auto"}`, string(parts[1]))
}

func TestUploadFailureIsNotCachedAndIsolatedByNamespace(t *testing.T) {
	failing := errors.New("upload down")
	_, err := RewriteInlineImages(context.Background(), mustJSON(t, map[string]any{"input": []any{
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": imageDataURL()}}},
	}}), "ns-fail", func(context.Context, string, []byte) (string, error) {
		return "", failing
	})
	require.ErrorIs(t, err, failing)

	calls := 0
	_, err = RewriteInlineImages(context.Background(), mustJSON(t, map[string]any{"input": []any{
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": imageDataURL()}}},
	}}), "ns-fail", func(context.Context, string, []byte) (string, error) {
		calls++
		return "file-retry", nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls, "failed uploads must not be cached")

	_, err = RewriteInlineImages(context.Background(), mustJSON(t, map[string]any{"input": []any{
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": imageDataURL()}}},
	}}), "ns-other", func(context.Context, string, []byte) (string, error) {
		calls++
		return "file-ns", nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls, "cache must be isolated per endpoint and credential")
}

func TestUploadCacheCoalescesConcurrentUploads(t *testing.T) {
	var uploads atomic.Int32
	release := make(chan struct{})
	var group sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			out, err := RewriteInlineImages(context.Background(), mustJSON(t, map[string]any{"input": []any{
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": imageDataURL()}}},
			}}), "ns-concurrent", func(context.Context, string, []byte) (string, error) {
				uploads.Add(1)
				<-release
				return "file-concurrent", nil
			})
			if err != nil {
				errs <- err
				return
			}
			root, err := parseObject(out)
			if err != nil {
				errs <- err
				return
			}
			var items []json.RawMessage
			if err := json.Unmarshal(root["input"], &items); err != nil {
				errs <- err
				return
			}
			item, _ := parseObject(items[0])
			var parts []json.RawMessage
			if err := json.Unmarshal(item["content"], &parts); err != nil {
				errs <- err
				return
			}
			part, _ := parseObject(parts[0])
			ids <- stringValue(part["file_id"])
		}()
	}
	// Let the first upload start, then release it once everyone else is waiting.
	for uploads.Load() == 0 {
	}
	close(release)
	group.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for id := range ids {
		require.Equal(t, "file-concurrent", id)
	}
	require.Equal(t, int32(1), uploads.Load(), "concurrent identical images must coalesce into one upload")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func TestStripEncryptedContentDeep(t *testing.T) {
	body := mustJSON(t, map[string]any{"input": []any{
		map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": "gAAA=="},
		map[string]any{"type": "function_call_output", "call_id": "c1", "output": []any{
			map[string]any{"type": "output_text", "text": "ok", "encrypted_content": "nested-blob"},
		}, "encrypted_content": "top-blob"},
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": "hi", "encrypted_content": "deep"},
		}},
	}})
	stripped, err := StripEncryptedContent(body)
	require.NoError(t, err)
	require.NotContains(t, string(stripped), "encrypted_content")
	require.NotContains(t, string(stripped), "gAAA==")
	require.NotContains(t, string(stripped), "nested-blob")
	require.NotContains(t, string(stripped), "top-blob")
	require.Contains(t, string(stripped), `"ok"`, "readable output survives")
	require.Contains(t, string(stripped), "hi", "text survives")

	// Nothing to strip: returned unchanged.
	clean := mustJSON(t, map[string]any{"input": []any{
		map[string]any{"type": "message", "role": "user", "content": "hi"},
	}})
	out, err := StripEncryptedContent(clean)
	require.NoError(t, err)
	require.JSONEq(t, string(clean), string(out))
}
