package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrLedgerEntryNotFound = errors.New("ledger entry not found")

type LedgerEntry struct {
	ID                     string
	WalletID               string
	TransactionID          string
	AmountDeltaMinorUnits  int64
	BalanceAfterMinorUnits int64
	CreatedAt              time.Time
}

type LedgerRepository struct {
	db *sql.DB
}

func NewLedgerRepository(db *sql.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// Append inserts a ledger entry using the caller's database transaction.
func (r *LedgerRepository) Append(
	ctx context.Context,
	tx *sql.Tx,
	entry LedgerEntry,
) error {
	if tx == nil ||
		entry.ID == "" ||
		entry.WalletID == "" ||
		entry.TransactionID == "" ||
		entry.AmountDeltaMinorUnits == 0 ||
		entry.BalanceAfterMinorUnits < 0 {
		return errors.New("invalid ledger entry")
	}

	const query = `
		INSERT INTO ledger_entries (
			id,
			wallet_id,
			transaction_id,
			amount_delta_minor_units,
			balance_after_minor_units
		)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := tx.ExecContext(
		ctx,
		query,
		entry.ID,
		entry.WalletID,
		entry.TransactionID,
		entry.AmountDeltaMinorUnits,
		entry.BalanceAfterMinorUnits,
	)
	if err != nil {
		return fmt.Errorf("append ledger entry: %w", err)
	}

	return nil
}

// GetByTransactionID retrieves the ledger entry for a transaction.
func (r *LedgerRepository) GetByTransactionID(
	ctx context.Context,
	transactionID string,
) (LedgerEntry, error) {
	const query = `
		SELECT
			id,
			wallet_id,
			transaction_id,
			amount_delta_minor_units,
			balance_after_minor_units,
			created_at
		FROM ledger_entries
		WHERE transaction_id = $1
	`

	var entry LedgerEntry

	err := r.db.QueryRowContext(ctx, query, transactionID).Scan(
		&entry.ID,
		&entry.WalletID,
		&entry.TransactionID,
		&entry.AmountDeltaMinorUnits,
		&entry.BalanceAfterMinorUnits,
		&entry.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return LedgerEntry{}, ErrLedgerEntryNotFound
	}
	if err != nil {
		return LedgerEntry{}, fmt.Errorf("get ledger entry by transaction ID: %w", err)
	}

	return entry, nil
}
