package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrInboxPayloadMismatch = errors.New("inbox message redelivered with a different payload")

type InboxRepository struct {
	db *sql.DB
}

func NewInboxRepository(db *sql.DB) *InboxRepository {
	return &InboxRepository{db: db}
}

// Record registers a message's arrival idempotently. It reports whether the
// message had already been completed by a previous delivery, so the caller
// can skip reprocessing and just acknowledge it.
func (r *InboxRepository) Record(
	ctx context.Context,
	tx *sql.Tx,
	id string,
	consumerName string,
	messageID string,
	payloadHash string,
) (alreadyCompleted bool, err error) {
	if tx == nil {
		return false, errors.New("record inbox message: nil database transaction")
	}

	const insert = `
		INSERT INTO inbox_messages (id, consumer_name, message_id, payload_hash)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (consumer_name, message_id) DO NOTHING
	`

	result, err := tx.ExecContext(ctx, insert, id, consumerName, messageID, payloadHash)
	if err != nil {
		return false, fmt.Errorf("record inbox message: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check inbox insert: %w", err)
	}
	if rowsAffected == 1 {
		return false, nil
	}

	var existingHash string
	var completedAt sql.NullTime
	err = tx.QueryRowContext(
		ctx,
		`SELECT payload_hash, completed_at FROM inbox_messages
		 WHERE consumer_name = $1 AND message_id = $2`,
		consumerName,
		messageID,
	).Scan(&existingHash, &completedAt)
	if err != nil {
		return false, fmt.Errorf("inspect existing inbox message: %w", err)
	}

	if existingHash != payloadHash {
		return false, ErrInboxPayloadMismatch
	}

	return completedAt.Valid, nil
}

// Complete marks a message as durably handled, so a future redelivery can be
// acknowledged without reapplying business logic.
func (r *InboxRepository) Complete(
	ctx context.Context,
	tx *sql.Tx,
	consumerName string,
	messageID string,
) error {
	if tx == nil {
		return errors.New("complete inbox message: nil database transaction")
	}

	_, err := tx.ExecContext(
		ctx,
		`UPDATE inbox_messages SET completed_at = NOW()
		 WHERE consumer_name = $1 AND message_id = $2`,
		consumerName,
		messageID,
	)
	if err != nil {
		return fmt.Errorf("complete inbox message: %w", err)
	}

	return nil
}
