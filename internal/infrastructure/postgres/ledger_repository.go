package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/ledger"
	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/money"
)

var ErrLedgerEntryNotFound = errors.New("ledger entry not found")

type LedgerRepository struct {
	db *sql.DB
}

func NewLedgerRepository(db *sql.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// Append inserts a ledger entry using the caller's database transaction.
// The amount_delta column keeps a signed value (negative for debit) to make
// reconciliation sums (credits - debits) a plain SUM() query.
func (r *LedgerRepository) Append(
	ctx context.Context,
	tx *sql.Tx,
	entry ledger.Entry,
) error {
	if tx == nil {
		return errors.New("append ledger entry: nil database transaction")
	}

	delta := entry.Amount().AmountInMinorUnits()
	if entry.Direction() == ledger.DirectionDebit {
		delta = -delta
	}

	const query = `
		INSERT INTO ledger_entries (
			id,
			wallet_id,
			transaction_id,
			direction,
			amount_delta_minor_units,
			balance_before_minor_units,
			balance_after_minor_units
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := tx.ExecContext(
		ctx,
		query,
		entry.ID(),
		entry.WalletID(),
		entry.TransactionID(),
		string(entry.Direction()),
		delta,
		entry.BalanceBefore().AmountInMinorUnits(),
		entry.BalanceAfter().AmountInMinorUnits(),
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
) (ledger.Entry, error) {
	const query = `
		SELECT
			le.id,
			le.wallet_id,
			le.transaction_id,
			le.direction,
			le.balance_before_minor_units,
			le.balance_after_minor_units,
			le.created_at,
			w.currency
		FROM ledger_entries le
		JOIN wallets w ON w.id = le.wallet_id
		WHERE le.transaction_id = $1
	`

	return r.scanEntry(r.db.QueryRowContext(ctx, query, transactionID))
}

// ListByWallet returns ledger entries for a wallet, oldest first, for
// cursor-based pagination and reconciliation. When afterID is non-empty, it
// resumes strictly after that entry's (createdAt, id) position.
func (r *LedgerRepository) ListByWallet(
	ctx context.Context,
	walletID string,
	afterCreatedAt time.Time,
	afterID string,
	limit int,
) ([]ledger.Entry, error) {
	const query = `
		SELECT
			le.id,
			le.wallet_id,
			le.transaction_id,
			le.direction,
			le.balance_before_minor_units,
			le.balance_after_minor_units,
			le.created_at,
			w.currency
		FROM ledger_entries le
		JOIN wallets w ON w.id = le.wallet_id
		WHERE le.wallet_id = $1
		  AND (
		    $2::uuid IS NULL
		    OR le.created_at > $3
		    OR (le.created_at = $3 AND le.id > $2::uuid)
		  )
		ORDER BY le.created_at, le.id
		LIMIT $4
	`

	var afterIDParam any
	if afterID != "" {
		afterIDParam = afterID
	}

	rows, err := r.db.QueryContext(ctx, query, walletID, afterIDParam, afterCreatedAt, limit)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []ledger.Entry
	for rows.Next() {
		entry, err := r.scanEntryRow(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger entries: %w", err)
	}

	return entries, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func (r *LedgerRepository) scanEntry(row scannable) (ledger.Entry, error) {
	entry, err := r.scanEntryRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.Entry{}, ErrLedgerEntryNotFound
	}
	return entry, err
}

func (r *LedgerRepository) scanEntryRow(row scannable) (ledger.Entry, error) {
	var (
		id            string
		walletID      string
		transactionID string
		direction     string
		balanceBefore int64
		balanceAfter  int64
		createdAt     sql.NullTime
		currency      string
	)

	err := row.Scan(
		&id,
		&walletID,
		&transactionID,
		&direction,
		&balanceBefore,
		&balanceAfter,
		&createdAt,
		&currency,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ledger.Entry{}, err
		}
		return ledger.Entry{}, fmt.Errorf("get ledger entry: %w", err)
	}

	before, err := money.New(formatMinorUnits(balanceBefore), currency)
	if err != nil {
		return ledger.Entry{}, fmt.Errorf("reconstruct ledger balance before: %w", err)
	}
	after, err := money.New(formatMinorUnits(balanceAfter), currency)
	if err != nil {
		return ledger.Entry{}, fmt.Errorf("reconstruct ledger balance after: %w", err)
	}

	var amount money.Money
	switch ledger.Direction(direction) {
	case ledger.DirectionDebit:
		amount, err = before.Subtract(after)
	case ledger.DirectionCredit:
		amount, err = after.Subtract(before)
	default:
		return ledger.Entry{}, fmt.Errorf("unknown ledger direction: %s", direction)
	}
	if err != nil {
		return ledger.Entry{}, fmt.Errorf("reconstruct ledger amount: %w", err)
	}

	return ledger.New(
		id, walletID, transactionID,
		ledger.Direction(direction),
		amount, before, after,
		createdAt.Time,
	)
}

func formatMinorUnits(units int64) string {
	return fmt.Sprintf("%d.%02d", units/100, units%100)
}
