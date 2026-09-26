package service

import (
	"context"
	"io"
	"log/slog"

	hclog "github.com/hashicorp/go-hclog"
)

// go-plugin already parses hclog JSON from the subprocess's original stderr.
// Reuse its sink API to preserve fields and levels in our existing log pipeline
// (console, rotating files and Ops), instead of dumping unstructured stderr.
func newPluginRuntimeLogger(installation *PluginInstallation) hclog.Logger {
	logger := hclog.NewInterceptLogger(&hclog.LoggerOptions{Name: "plugin", Level: hclog.Info, Output: io.Discard})
	logger.RegisterSink(pluginLogSink{logger: slog.Default().With("component", "plugin",
		"plugin_id", installation.PluginKey, "plugin_version", installation.Version)})
	return logger
}

type pluginLogSink struct{ logger *slog.Logger }

func (s pluginLogSink) Accept(_ string, level hclog.Level, message string, args ...any) {
	var severity slog.Level
	switch level {
	case hclog.Info:
		severity = slog.LevelInfo
	case hclog.Warn:
		severity = slog.LevelWarn
	case hclog.Error:
		severity = slog.LevelError
	default:
		return // Framework trace/debug output stays disabled.
	}
	s.logger.Log(context.Background(), severity, message, args...)
}
