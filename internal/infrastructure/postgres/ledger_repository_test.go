package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"
)

func setupLedgerTest(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	t.Cleanup(cancel)

	db, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close PostgreSQL: %v", err)
		}
	})

	return db, ctx
}

func TestLedgerRepositoryAppendAndGetByTransactionID(t *testing.T) {
	db, ctx := setupLedgerTest(t)
	repository := NewLedgerRepository(db)

	walletID := "51111111-1111-4111-8111-111111111111"
	transactionID := "52222222-2222-4222-8222-222222222222"
	entryID := "53333333-3333-4333-8333-333333333333"
	playerID := "54444444-4444-4444-8444-444444444444"

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	// Always roll back test data, including the append-only ledger entry.
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil &&
			!errors.Is(err, sql.ErrTxDone) {
			t.Errorf("rollback test transaction: %v", err)
		}
	})

	_, err = tx.ExecContext(ctx, `
		INSERT INTO wallets (
			id, player_id, currency, balance_minor_units, version
		)
		VALUES ($1, $2, 'BRL', 8000, 2)
	`, walletID, playerID)
	if err != nil {
		t.Fatalf("insert test wallet: %v", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO transactions (
			id, provider_id, external_id, idempotency_key, payload_hash,
			wallet_id, player_id, round_id, game_id, kind,
			amount_minor_units, status
		)
		VALUES (
			$1, 'test-provider', $2, $3, 'test-hash',
			$4, $5, 'round-1', 'game-1', 'BET', 1000, 'PROCESSED'
		)
	`, transactionID, transactionID, transactionID, walletID, playerID)
	if err != nil {
		t.Fatalf("insert test transaction: %v", err)
	}

	entry := LedgerEntry{
		ID:                     entryID,
		WalletID:               walletID,
		TransactionID:          transactionID,
		AmountDeltaMinorUnits:  -1000,
		BalanceAfterMinorUnits: 7000,
	}

	if err := repository.Append(ctx, tx, entry); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var got LedgerEntry

	err = tx.QueryRowContext(ctx, `
		SELECT
			id,
			wallet_id,
			transaction_id,
			amount_delta_minor_units,
			balance_after_minor_units,
			created_at
		FROM ledger_entries
		WHERE transaction_id = $1
	`, transactionID).Scan(
		&got.ID,
		&got.WalletID,
		&got.TransactionID,
		&got.AmountDeltaMinorUnits,
		&got.BalanceAfterMinorUnits,
		&got.CreatedAt,
	)
	if err != nil {
		t.Fatalf("read appended ledger entry: %v", err)
	}

	if got.ID != entryID {
		t.Errorf("ID = %q, want %q", got.ID, entryID)
	}
	if got.WalletID != walletID {
		t.Errorf("WalletID = %q, want %q", got.WalletID, walletID)
	}
	if got.TransactionID != transactionID {
		t.Errorf(
			"TransactionID = %q, want %q",
			got.TransactionID,
			transactionID,
		)
	}
	if got.AmountDeltaMinorUnits != -1000 {
		t.Errorf(
			"AmountDeltaMinorUnits = %d, want %d",
			got.AmountDeltaMinorUnits,
			-1000,
		)
	}
	if got.BalanceAfterMinorUnits != 7000 {
		t.Errorf(
			"BalanceAfterMinorUnits = %d, want %d",
			got.BalanceAfterMinorUnits,
			7000,
		)
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated")
	}
}

func TestLedgerRepositoryGetByTransactionIDNotFound(t *testing.T) {
	db, ctx := setupLedgerTest(t)
	repository := NewLedgerRepository(db)

	_, err := repository.GetByTransactionID(
		ctx,
		"49999999-9999-4999-8999-999999999999",
	)

	if !errors.Is(err, ErrLedgerEntryNotFound) {
		t.Fatalf(
			"error = %v, want ErrLedgerEntryNotFound",
			err,
		)
	}
}
