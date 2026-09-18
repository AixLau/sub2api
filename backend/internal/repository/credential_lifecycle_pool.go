package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// Reserve two connections OUT OF the configured process budget, not in addition
// to it. Admission is limited to one transaction per store plus the shared
// PostgreSQL try-lock. Critical operations retain the original row-lock order.
func ProvidePrincipalAdmissionStore(db *sql.DB, cfg *config.Config) (service.PrincipalAdmissionStore, error) {
	store := &principalAdmissionStore{db: db}
	if !cfg.Gateway.MultiCredentialHTTPEnabled {
		return store, nil
	}
	if cfg.Database.MaxOpenConns < 4 {
		return nil, errors.New("grouped HTTP requires a database budget of at least four connections")
	}
	connector, err := pq.NewConnector(cfg.Database.DSNWithTimezone(cfg.Timezone))
	if err != nil {
		return nil, err
	}
	critical := sql.OpenDB(connector)
	critical.SetMaxOpenConns(2)
	critical.SetMaxIdleConns(2)
	settings := clampDBPoolSettings(cfg)
	critical.SetConnMaxLifetime(settings.ConnMaxLifetime)
	critical.SetConnMaxIdleTime(settings.ConnMaxIdleTime)
	ctx, end := context.WithTimeout(context.Background(), 5*time.Second)
	defer end()
	if err = critical.PingContext(ctx); err != nil {
		_ = critical.Close()
		return nil, err
	}
	db.SetMaxOpenConns(cfg.Database.MaxOpenConns - 2)
	db.SetMaxIdleConns(min(cfg.Database.MaxIdleConns, cfg.Database.MaxOpenConns-2))
	store.lifecycleDB = critical
	return store, nil
}

func ProvideCredentialOperations(store service.PrincipalAdmissionStore) service.CredentialOperations {
	return NewCredentialOperations(store.(*principalAdmissionStore).criticalDB())
}

func (s *principalAdmissionStore) Close() error {
	if s.lifecycleDB != nil {
		return s.lifecycleDB.Close()
	}
	return nil
}
