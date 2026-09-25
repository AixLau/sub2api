package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBPSModelSelectionDefaultsAndRoundTrip(t *testing.T) {
	cfg, normalized, err := Parse([]byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, BPSModelModeAll, cfg.BPSModelMode)
	require.True(t, cfg.AllowsBPSModel("any-model"))
	require.Contains(t, string(normalized), `"bps_models":[]`)

	cfg, normalized, err = Parse([]byte(`{"bps_model_mode":"selected","bps_models":[" model-a ","model-a","model-b-excel"],"model_mapping":{"model-b-excel":"model-b"}}`))
	require.NoError(t, err)
	require.Equal(t, []string{"model-a", "model-b-excel"}, cfg.BPSModels)
	require.True(t, cfg.AllowsBPSModel("model-a"))
	require.True(t, cfg.AllowsBPSModel("model-b-excel"))
	for _, model := range []string{"Model-a", "model-b", "model-c", ""} {
		require.False(t, cfg.AllowsBPSModel(model), model)
	}
	roundTrip, _, err := Parse(normalized)
	require.NoError(t, err)
	require.Equal(t, cfg, roundTrip)

	for _, raw := range []string{`{"bps_model_mode":"selected"}`, `{"bps_model_mode":"selected","bps_models":null}`} {
		cfg, normalized, err = Parse([]byte(raw))
		require.NoError(t, err)
		require.False(t, cfg.AllowsBPSModel("model-a"))
		require.Contains(t, string(normalized), `"bps_models":[]`)
	}
}

func TestBPSModelSelectionRejectsInvalidConfiguration(t *testing.T) {
	for _, raw := range []string{
		`{"bps_model_mode":""}`,
		`{"bps_model_mode":"unknown"}`,
		`{"bps_models":"model-a"}`,
		`{"bps_models":[42]}`,
		`{"bps_models":[" "]}`,
		`{"bps_models":["model a"]}`,
		`{"bps_models":["model*"]}`,
		`{"bps_models":["model?"]}`,
	} {
		_, _, err := Parse([]byte(raw))
		require.Error(t, err, raw)
	}
	tooLong, err := json.Marshal(map[string]any{"bps_models": []string{strings.Repeat("x", maxModelName+1)}})
	require.NoError(t, err)
	_, _, err = Parse(tooLong)
	require.Error(t, err)
	models := make([]string, maxBPSModels+1)
	for i := range models {
		models[i] = fmt.Sprintf("model-%d", i)
	}
	tooMany, err := json.Marshal(map[string]any{"bps_models": models})
	require.NoError(t, err)
	_, _, err = Parse(tooMany)
	require.ErrorContains(t, err, "最多允许 64")
}
