// credential-vault-rotate re-encrypts the offline credential vault. Secret key
// material is accepted only from mode-0600 files, never command-line arguments.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/credentialfence"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
)

type keyBundle struct {
	EncryptionKey  string `json:"credential_vault_key"`
	FingerprintKey string `json:"credential_fingerprint_key"`
}

func readPrivateFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return nil, errors.New("private key file required")
	}
	data := make([]byte, info.Size())
	n, err := io.ReadFull(f, data)
	if err != nil || int64(n) != info.Size() {
		return nil, errors.New("key file read failed")
	}
	return data, nil
}
func stageBundle(path string, bundle keyBundle) error {
	// Stage BEFORE changing the database. On any later error retain this file and
	// keep nodes fenced until the database key ID resolves the uncertain commit.
	data, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	defer clear(data)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		previous, readErr := readPrivateFile(path)
		if readErr != nil {
			return readErr
		}
		defer clear(previous)
		var existing keyBundle
		if json.Unmarshal(previous, &existing) != nil || existing != bundle {
			return errors.New("staged bundle differs")
		}
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func run(ctx context.Context) error {
	oldFile := flag.String("current-keys-file", "", "mode-0600 JSON current key bundle")
	newFile := flag.String("new-encryption-key-file", "", "mode-0600 file containing new 32-byte hex key")
	stagedFile := flag.String("staged-keys-file", "", "new mode-0600 JSON bundle, retained on failure")
	actor := flag.Int64("actor", 0, "administrator ID")
	operation := flag.String("operation-id", "", "stable UUID; reuse after uncertain commit")
	project := flag.String("compose-project", "", "exact local test/maintenance Compose project")
	gateway := flag.String("gateway-service", "", "all credential-holding gateway and worker containers")
	flag.Parse()
	current, err := readPrivateFile(*oldFile)
	if err != nil {
		return err
	}
	defer clear(current)
	var bundle keyBundle
	if json.Unmarshal(current, &bundle) != nil {
		return errors.New("invalid key bundle")
	}
	old, err := service.NewCredentialVaultWithFingerprintKey(bundle.EncryptionKey, bundle.FingerprintKey)
	if err != nil {
		return err
	}
	newKey, err := readPrivateFile(*newFile)
	if err != nil {
		return err
	}
	defer clear(newKey)
	bundle = keyBundle{EncryptionKey: strings.TrimSpace(string(newKey)), FingerprintKey: old.FingerprintKeyHex()}
	next, err := service.NewCredentialVaultWithFingerprintKey(bundle.EncryptionKey, bundle.FingerprintKey)
	if err != nil {
		return err
	}
	if err = stageBundle(*stagedFile, bundle); err != nil {
		return err
	}
	db, err := sql.Open("postgres", os.Getenv("SUB2API_CREDENTIAL_CONTROL_DSN"))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rotation := repository.NewCredentialVaultRotation(db, credentialfence.Docker{Project: *project, Service: *gateway})
	counts, err := rotation.Rotate(ctx, *actor, *operation, old, next)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		State           string           `json:"state"`
		EncryptionKeyID string           `json:"encryption_key_id"`
		Counts          map[string]int64 `json:"ciphertext_counts"`
	}{"RE_ENCRYPTED_GATEWAYS_REMAIN_FENCED", next.EncryptionKeyID(), counts})
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if run(ctx) != nil {
		fmt.Fprintln(os.Stderr, "credential key rotation rejected or commit unconfirmed; keep gateways fenced, retain both key bundles, retry the same operation ID; no credential material logged")
		os.Exit(1)
	}
}
