package repository

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type credentialTokenAlias struct{ fingerprint, kind string }

func credentialResultAliases(result service.CredentialRefreshResult) []credentialTokenAlias {
	var aliases []credentialTokenAlias
	for _, alias := range []credentialTokenAlias{{result.AccessFingerprint, "ACCESS"}, {result.RefreshFingerprint, "REFRESH"}} {
		if alias.fingerprint != "" {
			aliases = append(aliases, alias)
		}
	}
	sort.Slice(aliases, func(i, j int) bool {
		if aliases[i].fingerprint == aliases[j].fingerprint {
			return aliases[i].kind < aliases[j].kind
		}
		return aliases[i].fingerprint < aliases[j].fingerprint
	})
	return aliases
}

func putCredentialInstanceAliasClaims(ctx context.Context, q sqlExecutor, instance int64, aliases []credentialTokenAlias) error {
	for _, alias := range aliases {
		// Keep the first historical owner, including retired owners. Authorization
		// is determined by independent live claims, never by overwriting history.
		if _, err := q.ExecContext(ctx, `INSERT INTO credential_fingerprints(fingerprint,instance_id,kind) VALUES($1,$2,$3) ON CONFLICT(fingerprint) DO NOTHING`, alias.fingerprint, instance, alias.kind); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO credential_instance_alias_claims(instance_id,fingerprint,kind) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, instance, alias.fingerprint, alias.kind); err != nil {
			return err
		}
	}
	return nil
}

func putCredentialRefreshAliasClaims(ctx context.Context, q sqlExecutor, operation string, aliases []credentialTokenAlias) error {
	for _, alias := range aliases {
		if _, err := q.ExecContext(ctx, `INSERT INTO credential_refresh_alias_claims(operation_id,fingerprint,kind) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, operation, alias.fingerprint, alias.kind); err != nil {
			return err
		}
	}
	return nil
}

// hasForeignCredentialAliasClaims must run in the credential arbitration
// transaction. Callers already hold the shared arbitration lock and sorted
// token registry locks before taking any domain locks. This query does not grant
// ownership and does not alter another operation's claim, including same-instance
// operations. Imports are checked separately by the arbitration caller.
func hasForeignCredentialAliasClaims(ctx context.Context, q sqlExecutor, fingerprints []string, instanceID int64, operationID string) (bool, error) {
	if len(fingerprints) == 0 {
		return false, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM credential_fingerprints f JOIN credential_instances i ON i.id=f.instance_id
 WHERE f.fingerprint=ANY($1::text[]) AND f.kind IN ('ACCESS','REFRESH') AND NOT i.retired_to_legacy AND i.id<>$2
 UNION ALL
 SELECT 1 FROM credential_instance_alias_claims c JOIN credential_instances i ON i.id=c.instance_id
 WHERE c.fingerprint=ANY($1::text[]) AND NOT i.retired_to_legacy AND i.id<>$2
 UNION ALL
 SELECT 1 FROM credential_refresh_alias_claims c JOIN credential_refresh_ops o ON o.id=c.operation_id
 WHERE c.fingerprint=ANY($1::text[]) AND o.state IN ('SENDING','REFRESH_RESULT_UNKNOWN') AND o.id::text<>$3
 )`, pq.Array(fingerprints), instanceID, operationID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, err
		}
		return false, sql.ErrNoRows
	}
	var found bool
	err = rows.Scan(&found)
	return found, err
}

func credentialInstanceAliasFingerprints(ctx context.Context, q sqlExecutor, instanceID int64) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT fingerprint FROM credential_fingerprints WHERE instance_id=$1 AND kind IN ('ACCESS','REFRESH')
 UNION SELECT fingerprint FROM credential_instance_alias_claims WHERE instance_id=$1
 ORDER BY fingerprint`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fingerprints []string
	for rows.Next() {
		var fingerprint string
		if err := rows.Scan(&fingerprint); err != nil {
			return nil, err
		}
		fingerprints = append(fingerprints, fingerprint)
	}
	return fingerprints, rows.Err()
}

// backfillUnknownAliasClaims is called by offline/first-use arbitration
// initialization in its locked transaction. Any failed decryption rejects the
// initialization; callers must roll back instead of treating missing claims as
// proof that no conflict exists. Ciphertext, tokens and expiry are never changed.
func backfillUnknownAliasClaims(ctx context.Context, q sqlExecutor, vault *service.CredentialVault) error {
	cursor := ""
	for {
		rows, err := q.QueryContext(ctx, `SELECT id::text,result_aad,result_ciphertext FROM credential_refresh_ops
 WHERE id::text>$1 AND state IN ('SENDING','REFRESH_RESULT_UNKNOWN') AND octet_length(result_ciphertext)>0 ORDER BY id::text LIMIT 128`, cursor)
		if err != nil {
			return err
		}
		type result struct {
			id, aad string
			cipher  []byte
		}
		var batch []result
		for rows.Next() {
			var result result
			if err := rows.Scan(&result.id, &result.aad, &result.cipher); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, result)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		if vault == nil {
			return service.ErrCredentialVaultUnavailable
		}
		for _, result := range batch {
			secret, err := vault.Open(result.aad, result.cipher)
			if err != nil {
				return service.ErrCredentialVaultUnavailable
			}
			var aliases []credentialTokenAlias
			for _, token := range []struct{ value, kind string }{{secret.AccessToken, "ACCESS"}, {secret.RefreshToken, "REFRESH"}} {
				if token.value == "" {
					continue
				}
				aliases = append(aliases, credentialTokenAlias{vault.Fingerprint("token", token.value), token.kind})
				if trimmed := strings.TrimSpace(token.value); trimmed != "" && trimmed != token.value {
					aliases = append(aliases, credentialTokenAlias{vault.Fingerprint("token", trimmed), token.kind})
				}
			}
			if len(aliases) == 0 {
				return service.ErrCredentialVaultUnavailable
			}
			if err := putCredentialRefreshAliasClaims(ctx, q, result.id, aliases); err != nil {
				return err
			}
			cursor = result.id
		}
	}
}
