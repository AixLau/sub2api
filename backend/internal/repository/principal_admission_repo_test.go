package repository

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrincipalAdmissionDatabaseUnavailable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin().WillReturnError(errors.New("database disconnected"))
	store := NewPrincipalAdmissionStore(db)
	d, err := store.TryAdmit(context.Background(), service.AdmissionInput{RequestID: uuid.NewString(), OwnerNonce: uuid.NewString(), Node: "test", PayloadDigest: "digest", CandidateIDs: []int64{1}, Endpoint: "responses"})
	require.ErrorIs(t, err, service.ErrAdmissionStoreUnavailable)
	require.NotEqual(t, service.AdmissionAdmitted, d.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
