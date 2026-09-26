package transport

// Only routing inputs are exposed, never headers, credentials or proxy URLs.
type routingPolicySnapshot struct {
	ConfigRevision string   `json:"config_revision"`
	BPSModelMode   string   `json:"bps_model_mode"`
	BPSModels      []string `json:"bps_models"`
	NativeFallback bool     `json:"native_fallback"`
	ToolsViaNative bool     `json:"tools_via_native"`
}

func routingPolicy(state *runtimeState) routingPolicySnapshot {
	return routingPolicySnapshot{
		ConfigRevision: state.revision, BPSModelMode: state.cfg.BPSModelMode,
		BPSModels:      append([]string{}, state.cfg.BPSModels...),
		NativeFallback: state.cfg.NativeFallbackEnabled(), ToolsViaNative: state.cfg.ToolsViaNativeEnabled(),
	}
}
