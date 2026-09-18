package main

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialVaultPrivateKeyFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	bundle := keyBundle{EncryptionKey: "mock-encryption", FingerprintKey: "mock-fingerprint"}
	require.NoError(t, stageBundle(path, bundle))
	data, err := readPrivateFile(path)
	require.NoError(t, err)
	var got keyBundle
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, bundle, got)
	require.NoError(t, stageBundle(path, bundle))
	require.Error(t, stageBundle(path, keyBundle{EncryptionKey: "other"}))
	require.NoError(t, os.Chmod(path, 0644))
	_, err = readPrivateFile(path)
	require.Error(t, err)
}
