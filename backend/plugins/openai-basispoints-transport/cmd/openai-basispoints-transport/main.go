package main

import (
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/transport"
)

func main() {
	pluginv1.Serve(transport.New())
}
