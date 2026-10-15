package main

import (
	"go.uber.org/fx"

	walletapp "github.com/Ricardoleaom/backend-challenge-go/internal/application/wallet"
	"github.com/Ricardoleaom/backend-challenge-go/internal/config"
	"github.com/Ricardoleaom/backend-challenge-go/internal/httpapi"
	"github.com/Ricardoleaom/backend-challenge-go/internal/infrastructure/postgres"
	"github.com/Ricardoleaom/backend-challenge-go/internal/observability"
)

var configModule = fx.Module("config",
	fx.Provide(
		config.Load,
		newLogger,
		observability.New,
	),
)

// persistenceModule owns the database connection and every repository.
// Repositories are infrastructure, not domain: the domain packages
// (internal/domain/...) never import this module or Fx itself.
var persistenceModule = fx.Module("persistence",
	fx.Provide(
		newDB,
		postgres.NewWalletRepository,
		postgres.NewTransactionRepository,
		postgres.NewLedgerRepository,
		newInboxRepository,
		newOutboxRepository,
	),
)

// applicationModule wires the use cases. These depend on the domain and on
// the repositories above, never on HTTP, SQS or Fx types.
var applicationModule = fx.Module("application",
	fx.Provide(
		walletapp.NewService,
		newWagerService,
		newReferenceRetryWorker,
	),
)

var messagingModule = fx.Module("messaging",
	fx.Provide(
		newSQSClient,
		newSQSConsumer,
		newOutboxPublisher,
		newSQSReadiness,
	),
)

var httpModule = fx.Module("http",
	fx.Provide(
		newAuthenticator,
		newHTTPHandlerDeps,
		httpapi.NewHandler,
	),
)

func main() {
	fx.New(
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
	).Run()
}
