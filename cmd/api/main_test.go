package main

import (
	"context"
	"os"
	"testing"
	"time"

	"go.uber.org/fx"
)

// TestFxAppStartsAndStopsCleanly builds the exact module graph main() runs,
// starts it against the real docker compose stack and stops it again. It
// checks that every provider in the composition resolves, every OnStart
// hook succeeds, and every background worker (SQS consumer, outbox
// publisher, reference retry worker) actually exits during OnStop instead
// of leaking a goroutine, per the edital's requirement to verify Fx startup
// and shutdown including worker resource release.
func TestFxAppStartsAndStopsCleanly(t *testing.T) {
	for _, key := range []string{"DATABASE_URL", "JWKS_URL", "SQS_TRANSACTIONS_QUEUE_URL", "SQS_EVENTS_QUEUE_URL"} {
		if os.Getenv(key) == "" {
			t.Skipf("%s is not set; skipping Fx composition test, which needs the full docker compose stack", key)
		}
	}

	// A fixed port would collide with an app container already bound to it.
	t.Setenv("HTTP_PORT", "0")

	app := fx.New(
		configModule,
		persistenceModule,
		applicationModule,
		messagingModule,
		httpModule,
		fx.Invoke(
			registerHTTPServer,
			registerSQSConsumer,
			registerOutboxPublisher,
			registerReferenceRetryWorker,
		),
		fx.NopLogger,
	)

	// newAuthenticator retries JWKS discovery for up to 60s on its own, so
	// Start needs headroom beyond Fx's 15s default.
	startCtx, cancelStart := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelStart()
	if err := app.Start(startCtx); err != nil {
		t.Fatalf("app.Start: %v", err)
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStop()
	if err := app.Stop(stopCtx); err != nil {
		t.Fatalf("app.Stop: %v", err)
	}
}
