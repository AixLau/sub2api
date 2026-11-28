package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestClaimRejectsDottedWelcomeEmailBeforeCredit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT campaign_id FROM user_reward_grants`).WithArgs(int64(7), int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"campaign_id"}).AddRow(1))
	mock.ExpectQuery(`SELECT COALESCE\(system_key, ''\) FROM reward_campaigns`).WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"system_key"}).AddRow(service.RewardSystemCampaignWelcome))
	mock.ExpectQuery(`SELECT status, amount, expires_at, claimed_at, balance_after`).WithArgs(int64(7), int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "amount", "expires_at", "claimed_at", "balance_after"}).AddRow("pending", 1.5, nil, nil, nil))
	mock.ExpectQuery(`SELECT email FROM users WHERE id = \$1 AND deleted_at IS NULL FOR UPDATE`).WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("first.last@gmail.com"))
	mock.ExpectRollback()

	result, err := (&rewardRepository{db: db}).Claim(context.Background(), 3, 7, time.Now())
	require.ErrorIs(t, err, service.ErrRewardNotEligible)
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImportLegacyPendingSkipsDottedWelcomeEmail(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT email, welcome_reward_amount, surprise_reward_amount FROM users`).WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"email", "welcome_reward_amount", "surprise_reward_amount"}).AddRow("first.last@gmail.com", 1.5, 0))
	mock.ExpectCommit()

	err = (&rewardRepository{db: db}).ImportLegacyPending(context.Background(), 3, time.Now())
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
