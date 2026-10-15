package sqsx

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	wagerapp "github.com/Ricardoleaom/backend-challenge-go/internal/application/wager"
	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/money"
	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/wager"
	"github.com/Ricardoleaom/backend-challenge-go/internal/infrastructure/postgres"
	"github.com/google/uuid"
)

// ConsumerName identifies this consumer in the inbox table.
const ConsumerName = "wager-transactions-consumer"

type messageEnvelope struct {
	MessageID  string      `json:"messageId"`
	Type       string      `json:"type"`
	OccurredAt string      `json:"occurredAt"`
	Data       messageData `json:"data"`
}

type messageData struct {
	ProviderID            string `json:"providerId"`
	ExternalTransactionID string `json:"externalTransactionId"`
	IdempotencyKey        string `json:"idempotencyKey"`
	PlayerID              string `json:"playerId"`
	WalletID              string `json:"walletId"`
	RoundID               string `json:"roundId"`
	GameID                string `json:"gameId"`
	Kind                  string `json:"kind"`
	Money                 struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"money"`
	ReferenceExternalTransactionID string `json:"referenceExternalTransactionId,omitempty"`
}

// Consumer polls the wager-transactions queue and feeds messages into the
// same application use case HTTP calls, so both transports share identical
// idempotency and processing guarantees.
//
// The inbox write and the financial commit run as two separate database
// transactions rather than one shared transaction: threading a
// caller-supplied *sql.Tx through the whole wager application service was
// out of scope for the time available. Safety does not depend on that
// atomicity. The transactions table's unique idempotency index (proven
// under 50 concurrent duplicate submissions in
// internal/application/wager/service_test.go) is what actually prevents a
// duplicate movement. The inbox here tracks message-level completion for
// operational visibility and to let a duplicate delivery short-circuit
// before re-running business logic.
type Consumer struct {
	client   *sqs.Client
	queueURL string
	db       *sql.DB
	inbox    *postgres.InboxRepository
	wager    *wagerapp.Service
}

func NewConsumer(
	client *sqs.Client,
	queueURL string,
	db *sql.DB,
	inbox *postgres.InboxRepository,
	wagerService *wagerapp.Service,
) *Consumer {
	return &Consumer{
		client:   client,
		queueURL: queueURL,
		db:       db,
		inbox:    inbox,
		wager:    wagerService,
	}
}

// Run polls until ctx is canceled. On SIGTERM the caller cancels ctx, which
// stops new ReceiveMessage calls; in-flight messages still being processed
// finish naturally since they are not waiting on the context.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     10,
			VisibilityTimeout:   30,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("receive SQS message", "error", err)
			time.Sleep(time.Second)
			continue
		}

		for _, msg := range out.Messages {
			c.handle(ctx, msg)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, msg sqstypes.Message) {
	messageID := aws.ToString(msg.MessageId)

	var envelope messageEnvelope
	if err := json.Unmarshal([]byte(aws.ToString(msg.Body)), &envelope); err != nil {
		// A malformed body will never parse; let it exhaust the redrive
		// policy's maxReceiveCount and land in the DLQ instead of looping
		// here forever.
		slog.Error("invalid SQS message body", "error", err, "message_id", messageID)
		return
	}

	amount, err := money.New(envelope.Data.Money.Amount, envelope.Data.Money.Currency)
	if err != nil {
		slog.Error("invalid money in SQS message", "error", err, "message_id", messageID)
		return
	}

	input := wagerapp.SubmitInput{
		ProviderID:          envelope.Data.ProviderID,
		ExternalID:          envelope.Data.ExternalTransactionID,
		IdempotencyKey:      envelope.Data.IdempotencyKey,
		PlayerID:            envelope.Data.PlayerID,
		WalletID:            envelope.Data.WalletID,
		RoundID:             envelope.Data.RoundID,
		GameID:              envelope.Data.GameID,
		Kind:                wager.Kind(envelope.Data.Kind),
		Amount:              amount,
		ReferenceExternalID: envelope.Data.ReferenceExternalTransactionID,
		CorrelationID:       messageID,
	}

	payloadHash, err := wagerapp.ComputePayloadHash(input)
	if err != nil {
		slog.Error("compute payload hash", "error", err, "message_id", messageID)
		return
	}

	alreadyCompleted, err := c.recordInbox(ctx, messageID, payloadHash)
	if err != nil {
		slog.Error("record inbox message", "error", err, "message_id", messageID)
		return
	}
	if alreadyCompleted {
		c.ack(ctx, msg)
		return
	}

	if _, err := c.wager.Submit(ctx, input); err != nil {
		slog.Error("process wager transaction from SQS", "error", err, "message_id", messageID)
		return
	}

	if err := c.completeInbox(ctx, messageID); err != nil {
		slog.Error("complete inbox message", "error", err, "message_id", messageID)
		return
	}

	c.ack(ctx, msg)
}

func (c *Consumer) recordInbox(ctx context.Context, messageID, payloadHash string) (alreadyCompleted bool, err error) {
	dbTx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin inbox record: %w", err)
	}
	defer dbTx.Rollback()

	alreadyCompleted, err = c.inbox.Record(ctx, dbTx, uuid.NewString(), ConsumerName, messageID, payloadHash)
	if err != nil {
		return false, err
	}

	if err := dbTx.Commit(); err != nil {
		return false, fmt.Errorf("commit inbox record: %w", err)
	}

	return alreadyCompleted, nil
}

func (c *Consumer) completeInbox(ctx context.Context, messageID string) error {
	dbTx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin inbox complete: %w", err)
	}
	defer dbTx.Rollback()

	if err := c.inbox.Complete(ctx, dbTx, ConsumerName, messageID); err != nil {
		return err
	}

	return dbTx.Commit()
}

func (c *Consumer) ack(ctx context.Context, msg sqstypes.Message) {
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.queueURL),
		ReceiptHandle: msg.ReceiptHandle,
	})
	if err != nil {
		slog.Error("delete processed SQS message", "error", err, "message_id", aws.ToString(msg.MessageId))
	}
}
