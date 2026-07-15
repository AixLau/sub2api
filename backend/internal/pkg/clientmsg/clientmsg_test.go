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
			name: "request body limit pattern",
			in:   "Request body too large, limit is 50MB",
			want: "请求体过大，最大允许 50MB",
		},
		{
			name: "request memory budget exhausted",
			in:   "Request memory budget exhausted",
			want: "当前请求所需资源超过可用准入配额，请稍后重试",
		},
		{
			name: "concurrency exact",
			in:   "Too many concurrent requests, please retry later",
			want: "并发请求过多，请稍后重试",
		},
		{
			name: "local billing balance",
			in:   "insufficient balance",
			want: "当前账户余额不足，请充值后重试，充值地址：https://aixlau.me/purchase",
		},
		{
			name: "local billing unavailable",
			in:   "Billing service temporarily unavailable. Please retry later.",
			want: "计费服务暂时不可用，请稍后重试",
		},
		{
			name: "local platform daily quota",
			in:   "Daily usage quota exhausted for this platform.",
			want: "当前平台今日用量额度已用完，请在额度重置后重试",
		},
		{
			name: "local group rpm",
			in:   "group requests-per-minute limit exceeded",
			want: "当前分组请求频率过高，请稍后重试",
		},
		{
			name: "local concurrency pattern",
			in:   "Concurrency limit exceeded for user, please retry later",
			want: "当前用户并发请求已达上限，请稍后重试",
		},
		{
			name: "upstream status prefix",
			in:   "Upstream request failed (status 502)",
			want: "请求处理失败（状态码 502)",
		},
		{
			name: "upstream overloaded without topology",
			in:   "Upstream service overloaded, please retry later",
			want: "服务繁忙，请稍后重试",
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
