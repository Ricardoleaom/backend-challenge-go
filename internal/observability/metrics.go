// Package observability holds the Prometheus metrics the challenge asks
// for: outcomes by status, duplicates, retries, outbox lag and
// reconciliation divergences.
package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	Registry *prometheus.Registry

	HTTPRequestsTotal         *prometheus.CounterVec
	WagerTransactionsTotal    *prometheus.CounterVec
	IdempotentReplaysTotal    prometheus.Counter
	ConcurrencyConflictsTotal prometheus.Counter
	OutboxPublishedTotal      prometheus.Counter
	OutboxPublishFailedTotal  prometheus.Counter
	ReferenceRetriesTotal     prometheus.Counter
	ReconciliationMismatches  prometheus.Counter
}

func New() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		Registry: registry,
		HTTPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests by path, method and status.",
		}, []string{"path", "method", "status"}),
		WagerTransactionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wager_transactions_total",
			Help: "Processed wager transactions by resulting status.",
		}, []string{"status"}),
		IdempotentReplaysTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wager_idempotent_replays_total",
			Help: "Submissions that replayed a previously persisted result.",
		}),
		ConcurrencyConflictsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wager_concurrency_conflicts_total",
			Help: "Submissions that lost a race on the idempotency key and replayed the winner.",
		}),
		OutboxPublishedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "outbox_events_published_total",
			Help: "Outbox events successfully published.",
		}),
		OutboxPublishFailedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "outbox_events_publish_failed_total",
			Help: "Outbox publish attempts that failed and were rescheduled with backoff.",
		}),
		ReferenceRetriesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wager_reference_retries_total",
			Help: "PENDING_REFERENCE transactions retried by the background worker.",
		}),
		ReconciliationMismatches: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wallet_reconciliation_mismatches_total",
			Help: "Reconciliation checks that found stored and calculated balances differ.",
		}),
	}

	registry.MustRegister(
		m.HTTPRequestsTotal,
		m.WagerTransactionsTotal,
		m.IdempotentReplaysTotal,
		m.ConcurrencyConflictsTotal,
		m.OutboxPublishedTotal,
		m.OutboxPublishFailedTotal,
		m.ReferenceRetriesTotal,
		m.ReconciliationMismatches,
	)

	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}
