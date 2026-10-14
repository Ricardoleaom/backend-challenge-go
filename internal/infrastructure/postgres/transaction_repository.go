package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/money"
	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/wager"
)

var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrTransactionConflict = errors.New("transaction uniqueness conflict")
)

type TransactionRepository struct {
	db *sql.DB
}

func NewTransactionRepository(db *sql.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

// Create persists a transaction using the caller's database transaction.
func (r *TransactionRepository) Create(
	ctx context.Context,
	tx *sql.Tx,
	transaction wager.Transaction,
) error {
	if tx == nil {
		return errors.New("create transaction: nil database transaction")
	}

	const query = `
		INSERT INTO transactions (
			id,
			provider_id,
			external_id,
			idempotency_key,
			payload_hash,
			wallet_id,
			player_id,
			round_id,
			game_id,
			kind,
			amount_minor_units,
			reference_id,
			status,
			failure_code,
			created_at,
			updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, NULLIF($12, ''), $13,
			NULLIF($14, ''), $15, $16
		)
	`

	_, err := tx.ExecContext(
		ctx,
		query,
		transaction.ID(),
		transaction.ProviderID(),
		transaction.ExternalID(),
		transaction.IdempotencyKey(),
		transaction.PayloadHash(),
		transaction.WalletID(),
		transaction.PlayerID(),
		transaction.RoundID(),
		transaction.GameID(),
		string(transaction.Kind()),
		transaction.Amount().AmountInMinorUnits(),
		transaction.ReferenceID(),
		string(transaction.Status()),
		transaction.FailureCode(),
		transaction.CreatedAt(),
		transaction.UpdatedAt(),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: %s", ErrTransactionConflict, pgErr.ConstraintName)
		}

		return fmt.Errorf("create transaction: %w", err)
	}

	return nil
}

// GetByID retrieves a transaction and reconstructs its domain representation.
func (r *TransactionRepository) GetByID(
	ctx context.Context,
	id string,
) (wager.Transaction, error) {
	const query = `
		SELECT
			t.id,
			t.provider_id,
			t.external_id,
			t.idempotency_key,
			t.payload_hash,
			t.wallet_id,
			t.player_id,
			t.round_id,
			t.game_id,
			t.kind,
			t.amount_minor_units,
			w.currency,
			t.reference_id,
			t.status,
			t.failure_code,
			t.created_at,
			t.updated_at
		FROM transactions t
		JOIN wallets w ON w.id = t.wallet_id
		WHERE t.id = $1
	`

	var (
		input       wager.Input
		kind        string
		status      string
		amountUnits int64
		currency    string
		referenceID sql.NullString
		failureCode sql.NullString
		createdAt   sql.NullTime
		updatedAt   sql.NullTime
	)

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&input.ID,
		&input.ProviderID,
		&input.ExternalID,
		&input.IdempotencyKey,
		&input.PayloadHash,
		&input.WalletID,
		&input.PlayerID,
		&input.RoundID,
		&input.GameID,
		&kind,
		&amountUnits,
		&currency,
		&referenceID,
		&status,
		&failureCode,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return wager.Transaction{}, ErrTransactionNotFound
	}
	if err != nil {
		return wager.Transaction{}, fmt.Errorf("get transaction by ID: %w", err)
	}

	if amountUnits < 0 || !createdAt.Valid || !updatedAt.Valid {
		return wager.Transaction{}, errors.New("invalid persisted transaction data")
	}

	amountText := fmt.Sprintf("%d.%02d", amountUnits/100, amountUnits%100)
	input.Amount, err = money.New(amountText, currency)
	if err != nil {
		return wager.Transaction{}, fmt.Errorf("reconstruct transaction amount: %w", err)
	}

	input.Kind = wager.Kind(kind)
	input.ReferenceID = referenceID.String

	transaction, err := wager.Rehydrate(
		input,
		wager.Status(status),
		failureCode.String,
		createdAt.Time,
		updatedAt.Time,
	)
	if err != nil {
		return wager.Transaction{}, fmt.Errorf("reconstruct transaction: %w", err)
	}

	return transaction, nil
}

// GetByIdempotencyKey retrieves a transaction using its provider and key.
func (r *TransactionRepository) GetByIdempotencyKey(
	ctx context.Context,
	providerID string,
	idempotencyKey string,
) (wager.Transaction, error) {
	const query = `
		SELECT id
		FROM transactions
		WHERE provider_id = $1 AND idempotency_key = $2
	`

	var id string
	err := r.db.QueryRowContext(
		ctx, query, providerID, idempotencyKey,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return wager.Transaction{}, ErrTransactionNotFound
	}
	if err != nil {
		return wager.Transaction{}, fmt.Errorf("find transaction by idempotency key: %w", err)
	}

	return r.GetByID(ctx, id)
}

// GetByExternalID retrieves a transaction using its provider and external ID.
func (r *TransactionRepository) GetByExternalID(
	ctx context.Context,
	providerID string,
	externalID string,
) (wager.Transaction, error) {
	const query = `
		SELECT id
		FROM transactions
		WHERE provider_id = $1 AND external_id = $2
	`

	var id string
	err := r.db.QueryRowContext(
		ctx, query, providerID, externalID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return wager.Transaction{}, ErrTransactionNotFound
	}
	if err != nil {
		return wager.Transaction{}, fmt.Errorf("find transaction by external ID: %w", err)
	}

	return r.GetByID(ctx, id)
}

// UpdateStatus persists the current status of a transaction.
func (r *TransactionRepository) UpdateStatus(
	ctx context.Context,
	tx *sql.Tx,
	transaction wager.Transaction,
) error {
	if tx == nil {
		return errors.New("update transaction status: nil database transaction")
	}

	const query = `
        UPDATE transactions
        SET status = $1,
            failure_code = NULLIF($2, ''),
            updated_at = $3
        WHERE id = $4
    `

	result, err := tx.ExecContext(
		ctx,
		query,
		string(transaction.Status()),
		transaction.FailureCode(),
		transaction.UpdatedAt(),
		transaction.ID(),
	)
	if err != nil {
		return fmt.Errorf("update transaction status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check transaction status update: %w", err)
	}

	if rowsAffected == 0 {
		return ErrTransactionNotFound
	}

	return nil
}
