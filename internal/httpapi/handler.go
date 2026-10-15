package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"

	wagerapp "github.com/Ricardoleaom/backend-challenge-go/internal/application/wager"
	walletapp "github.com/Ricardoleaom/backend-challenge-go/internal/application/wallet"
	"github.com/Ricardoleaom/backend-challenge-go/internal/infrastructure/postgres"
	"github.com/Ricardoleaom/backend-challenge-go/internal/observability"
)

// ReadinessChecker reports whether a dependency is ready to serve traffic.
type ReadinessChecker interface {
	Name() string
	Ready(ctx context.Context) error
}

type Dependencies struct {
	DB                *sql.DB
	Wallets           *postgres.WalletRepository
	Transactions      *postgres.TransactionRepository
	Ledger            *postgres.LedgerRepository
	WalletService     *walletapp.Service
	WagerService      *wagerapp.Service
	Auth              *Authenticator
	Metrics           *observability.Metrics
	ReadinessCheckers []ReadinessChecker
}

type handler struct {
	deps Dependencies
}

func NewHandler(deps Dependencies) http.Handler {
	h := &handler{deps: deps}

	protected := http.NewServeMux()
	protected.HandleFunc("POST /wallets", h.openWallet)
	protected.HandleFunc("GET /wallets/{id}", h.getWallet)
	protected.HandleFunc("GET /wallets/{id}/ledger", h.getWalletLedger)
	protected.HandleFunc("POST /wallets/{id}/reconciliation", h.reconcileWallet)
	protected.HandleFunc("POST /wagering/transactions", h.submitWagerTransaction)
	protected.HandleFunc("GET /wagering/transactions/{id}", h.getTransaction)
	protected.HandleFunc(
		"GET /providers/{providerId}/wagering/transactions/{externalId}",
		h.getTransactionByExternalID,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", healthLive)
	mux.HandleFunc("GET /health/ready", h.healthReady)
	mux.Handle("GET /metrics", deps.Metrics.Handler())
	mux.Handle("/", deps.Auth.Middleware(protected))

	return metricsMiddleware(deps.Metrics, mux)
}

// metricsMiddleware records every response by route pattern, method and
// status, feeding http_requests_total.
func metricsMiddleware(metrics *observability.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		metrics.HTTPRequestsTotal.WithLabelValues(
			r.Pattern, r.Method, strconv.Itoa(recorder.status),
		).Inc()
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
