package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrInsufficientBalance    = errors.New("insufficient wallet balance")
	ErrWalletCurrencyMismatch = errors.New("wallet currency mismatch")
	ErrWalletVersionOverflow  = errors.New("wallet version overflow")
)

// DebitWallet atomically debits a wallet inside a database transaction.
func DebitWallet(
	ctx context.Context,
	tx *sql.Tx,
	walletID string,
	amountMinorUnits int64,
	currency string,
) (int64, int64, error) {
	if tx == nil || amountMinorUnits <= 0 || walletID == "" || currency == "" {
		return 0, 0, errors.New("invalid wallet debit input")
	}

	const query = `
		UPDATE wallets
		SET balance_minor_units = balance_minor_units - $1,
		    version = version + 1,
		    updated_at = NOW()
		WHERE id = $2
		  AND currency = $3
		  AND balance_minor_units >= $1
		  AND version < 9223372036854775807
		RETURNING balance_minor_units, version
	`

	var newBalance int64
	var newVersion int64

	err := tx.QueryRowContext(
		ctx,
		query,
		amountMinorUnits,
		walletID,
		currency,
	).Scan(&newBalance, &newVersion)

	if err == nil {
		return newBalance, newVersion, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf("debit wallet: %w", err)
	}

	var currentCurrency string
	var currentBalance int64
	var currentVersion int64

	err = tx.QueryRowContext(
		ctx,
		`SELECT currency, balance_minor_units, version
		 FROM wallets
		 WHERE id = $1`,
		walletID,
	).Scan(&currentCurrency, &currentBalance, &currentVersion)

	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrWalletNotFound
	}
	if err != nil {
		return 0, 0, fmt.Errorf("inspect wallet after rejected debit: %w", err)
	}

	if currentCurrency != currency {
		return 0, 0, ErrWalletCurrencyMismatch
	}
	if currentVersion == int64(^uint64(0)>>1) {
		return 0, 0, ErrWalletVersionOverflow
	}
	if currentBalance < amountMinorUnits {
		return 0, 0, ErrInsufficientBalance
	}

	return 0, 0, errors.New("wallet debit rejected by database")
}
