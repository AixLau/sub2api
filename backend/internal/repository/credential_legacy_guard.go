package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *accountRepository) KnownCredentialTokenFingerprints(ctx context.Context, fingerprints []string) (bool, error) {
	var known bool
	// Active known aliases include expired tokens: expiry must never permit old
	// refresh-token reuse. A drained rollback retires its own grouped carrier;
	// non-consumed imports still block duplicate legacy authorization.
	rows, err := r.sql.QueryContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM credential_fingerprints f JOIN credential_instances i ON i.id=f.instance_id
 WHERE NOT i.retired_to_legacy AND f.kind IN ('ACCESS','REFRESH') AND ($1::text[] IS NULL OR f.fingerprint=ANY($1))
 UNION ALL
 SELECT 1 FROM credential_instance_alias_claims c JOIN credential_instances i ON i.id=c.instance_id
 WHERE NOT i.retired_to_legacy AND ($1::text[] IS NULL OR c.fingerprint=ANY($1))
 UNION ALL
 SELECT 1 FROM credential_refresh_alias_claims c JOIN credential_refresh_ops o ON o.id=c.operation_id
 WHERE o.state IN ('SENDING','REFRESH_RESULT_UNKNOWN') AND ($1::text[] IS NULL OR c.fingerprint=ANY($1))
 UNION ALL
 SELECT 1 FROM credential_import_fingerprints f JOIN credential_imports i ON i.id=f.import_id
 WHERE f.kind='TOKEN' AND i.verification_state<>'CONSUMED' AND ($1::text[] IS NULL OR f.fingerprint=ANY($1))
 )`, pq.Array(fingerprints))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, service.ErrCredentialVaultUnavailable
	}
	err = rows.Scan(&known)
	return known, err
}
