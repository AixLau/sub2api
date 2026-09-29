package bridge

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

func TestReadRemoteCompactionResponse(t *testing.T) {
	const item = `{"type":"compaction","encrypted_content":"opaque"}`
	event := func(raw string) string { return "data: " + raw + "\n\n" }
	done := func(raw string) string { return event(`{"type":"response.output_item.done","item":` + raw + `}`) }
	completed := event(`{"type":"response.completed","response":{"id":"resp_compaction","output":[]}}`)
	snapshotOnly := event(`{"type":"response.completed","response":{"id":"resp_compaction","output":[` + item + `]}}`)
	failed := event(`{"type":"response.failed","response":{"id":"resp_compaction","status":"failed","error":{"code":"context_length_exceeded","message":"too long"}}}`)
	for _, tc := range []struct {
		name, body, mime, wantBody string
		wantErr                    error
	}{
		{name: "sse valid done then completed", body: done(item) + completed},
		{name: "sse comments and multiline data", body: ": progress\n\nevent: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\ndata: \"item\":" + item + "}\n\n" + completed},
		{name: "sse CRLF byte identical", body: strings.ReplaceAll(done(item)+completed, "\n", "\r\n")},
		{name: "sse ignores trailing events", body: done(item) + completed + done(item), wantBody: done(item) + completed},
		{name: "sse snapshot cannot replace done", body: snapshotOnly, wantErr: ErrCompactionResponseInvalid},
		{name: "sse completed before done", body: completed + done(item), wantErr: ErrCompactionResponseInvalid},
		{name: "sse duplicate done", body: done(item) + done(item) + completed, wantErr: ErrCompactionResponseInvalid},
		{name: "sse added cannot replace done", body: event(`{"type":"response.output_item.added","item":`+item+`}`) + completed, wantErr: ErrCompactionResponseInvalid},
		{name: "sse missing encrypted content", body: done(`{"type":"compaction"}`) + completed, wantErr: ErrCompactionResponseInvalid},
		{name: "sse null encrypted content", body: done(`{"type":"compaction","encrypted_content":null}`) + completed, wantErr: ErrCompactionResponseInvalid},
		{name: "sse numeric encrypted content", body: done(`{"type":"compaction","encrypted_content":1}`) + completed, wantErr: ErrCompactionResponseInvalid},
		{name: "sse invalid optional item id", body: done(`{"type":"compaction","id":3,"encrypted_content":"opaque"}`) + completed, wantErr: ErrCompactionResponseInvalid},
		{name: "sse missing response id", body: done(item) + event(`{"type":"response.completed","response":{}}`), wantErr: ErrCompactionResponseInvalid},
		{name: "sse invalid response id", body: done(item) + event(`{"type":"response.completed","response":{"id":42}}`), wantErr: ErrCompactionResponseInvalid},
		{name: "sse done only EOF", body: done(item), wantErr: io.ErrUnexpectedEOF},
		{name: "sse DONE is not completed", body: done(item) + event(`[DONE]`), wantErr: io.ErrUnexpectedEOF},
		{name: "sse truncated terminal frame", body: done(item) + strings.TrimSuffix(completed, "\n"), wantErr: io.ErrUnexpectedEOF},
		{name: "sse failed preserves error", body: failed},
		{name: "sse failure before validation", body: done(item) + done(item) + failed},
		{name: "sse failure takes precedence over malformed item", body: done(`{"type":"compaction"}`) + failed},
		{name: "sse interrupted preserved", body: event(`{"type":"response.incomplete","response":{"id":"resp_compaction","status":"incomplete","incomplete_details":{"reason":"interrupted"}}}`)},
		{name: "sse incomplete preserved", body: event(`{"type":"response.incomplete","response":{"id":"resp_compaction","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`)},
		{name: "sse error preserved", body: event(`{"type":"error","error":{"code":"insufficient_quota","message":"quota exhausted"}}`)},
		{name: "json completed", mime: "application/json", body: `{"status":"completed","output":[` + item + `]}`},
		{name: "json failed preserves error", mime: "application/json", body: `{"status":"failed","error":{"code":"context_length_exceeded","message":"too long"}}`},
		{name: "json error preserved", mime: "application/json", body: `{"error":{"code":"insufficient_quota","message":"quota exhausted"}}`},
		{name: "json incomplete preserved", mime: "application/json", body: `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}`},
		{name: "json missing status", mime: "application/json", body: `{"output":[` + item + `]}`, wantErr: ErrCompactionResponseInvalid},
		{name: "json still in progress", mime: "application/json", body: `{"status":"in_progress","output":[` + item + `]}`, wantErr: ErrCompactionResponseInvalid},
		{name: "json ordinary summary", mime: "application/json", body: `{"status":"completed","output":[{"type":"message"}]}`, wantErr: ErrCompactionResponseInvalid},
		{name: "json duplicate", mime: "application/json", body: `{"status":"completed","output":[` + item + `,` + item + `]}`, wantErr: ErrCompactionResponseInvalid},
		{name: "json invalid item", mime: "application/json", body: `{"status":"completed","output":[{"type":"compaction"}]}`, wantErr: ErrCompactionResponseInvalid},
		{name: "json invalid body", mime: "application/json", body: `null`, wantErr: ErrCompactionResponseInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mime := tc.mime
			if mime == "" {
				mime = "text/event-stream; charset=utf-8"
			}
			for _, fragmented := range []bool{false, true} {
				var src io.Reader = strings.NewReader(tc.body)
				if fragmented {
					src = iotest.OneByteReader(src)
				}
				got, err := ReadRemoteCompactionResponse(src, mime)
				if tc.wantErr != nil {
					require.ErrorIs(t, err, tc.wantErr)
					require.Nil(t, got, "invalid or unfinished output must not be forwarded")
				} else {
					require.NoError(t, err)
					want := tc.wantBody
					if want == "" {
						want = tc.body
					}
					require.Equal(t, want, string(got))
				}
			}
		})
	}
}

func TestReadRemoteCompactionStopsBeforeAnotherRead(t *testing.T) {
	const body = "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\",\"encrypted_content\":\"opaque\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_c\"}}\n\n"
	lateErr := errors.New("reading after terminal")
	got, err := ReadRemoteCompactionResponse(io.MultiReader(strings.NewReader(body), iotest.ErrReader(lateErr)), "text/event-stream")
	require.NoError(t, err)
	require.Equal(t, body, string(got))
	_, err = ReadRemoteCompactionResponse(io.MultiReader(strings.NewReader("data: {}\n\n"), iotest.ErrReader(lateErr)), "text/event-stream")
	require.ErrorIs(t, err, lateErr, "transport failures must keep their error identity")
}

func TestReadRemoteCompactionSizeLimit(t *testing.T) {
	for _, mime := range []string{"text/event-stream", "application/json"} {
		t.Run(mime, func(t *testing.T) {
			src := &compactionLimitReader{remaining: MaxResponseBytes + 1}
			got, err := ReadRemoteCompactionResponse(src, mime)
			require.ErrorIs(t, err, ErrCompactionResponseInvalid)
			require.Nil(t, got)
			require.Zero(t, src.remaining)
		})
	}
}

type compactionLimitReader struct{ remaining int }

func (r *compactionLimitReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, errors.New("read past limit")
	}
	n := min(len(p), r.remaining)
	for i := range p[:n] {
		p[i] = ' '
	}
	r.remaining -= n
	return n, nil
}
