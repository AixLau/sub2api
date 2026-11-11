package main

import (
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-bps-local/internal/plugin"
)

func main() {
	pluginv1.Serve(plugin.New())
}
