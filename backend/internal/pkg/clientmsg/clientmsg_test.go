package clientmsg

import "testing"

func TestLocalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "exact",
			in:   "Invalid API key",
			want: "API Key 无效",
		},
		{
			name: "prefix",
			in:   "Invalid request: bad field",
			want: "请求无效：bad field",
		},
		{
			name: "unknown preserved",
			in:   "provider-specific upstream message",
			want: "provider-specific upstream message",
		},
		{
			name: "model not supported pattern",
			in:   `Model "gpt-test" is not supported by any configured account in this group`,
			want: `该分组中没有任何已配置账号支持模型 "gpt-test"`,
		},
		{
			name: "body size pattern",
			in:   "body size 1048577 exceeds limit 1048576",
			want: "请求体大小 1048577 超过限制 1048576",
		},
		{
			name: "concurrency exact",
			in:   "Too many concurrent requests, please retry later",
			want: "并发请求过多，请稍后重试",
		},
		{
			name: "upstream status prefix",
			in:   "Upstream request failed (status 502)",
			want: "上游请求失败（状态码 502)",
		},
		{
			name: "trim match preserves localized",
			in:   "  No available accounts  ",
			want: "暂无可用账号",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Localize(tt.in); got != tt.want {
				t.Fatalf("Localize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
