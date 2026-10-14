package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/money"
	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/wallet"
)

var ErrWalletNotFound = errors.New("wallet not found")

type WalletRepository struct {
	db *sql.DB
}

func NewWalletRepository(db *sql.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) GetByID(
	ctx context.Context,
	id string,
) (wallet.Wallet, error) {
	const query = `
		SELECT
			id,
			player_id,
			currency,
			balance_minor_units,
			version,
			created_at,
			updated_at
		FROM wallets
		WHERE id = $1
	`

	var (
		walletID  string
		playerID  string
		currency  string
		balance   int64
		version   int64
		createdAt sql.NullTime
		updatedAt sql.NullTime
	)

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&walletID,
		&playerID,
		&currency,
		&balance,
		&version,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return wallet.Wallet{}, ErrWalletNotFound
	}
	if err != nil {
		return wallet.Wallet{}, fmt.Errorf("get wallet by ID: %w", err)
	}

	if !createdAt.Valid || !updatedAt.Valid {
		return wallet.Wallet{}, fmt.Errorf(
			"get wallet by ID: missing timestamps",
		)
	}

	if balance < 0 {
		return wallet.Wallet{}, fmt.Errorf(
			"get wallet by ID: negative persisted balance",
		)
	}

	amount := fmt.Sprintf("%d.%02d", balance/100, balance%100)

	moneyBalance, err := money.New(amount, currency)
	if err != nil {
		return wallet.Wallet{}, fmt.Errorf(
			"reconstruct wallet balance: %w",
			err,
		)
	}

	result, err := wallet.Rehydrate(
		walletID,
		playerID,
		moneyBalance,
		version,
		createdAt.Time,
		updatedAt.Time,
	)
	if err != nil {
		return wallet.Wallet{}, fmt.Errorf(
			"reconstruct wallet: %w",
			err,
		)
	}

	return result, nil
}
