package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	walletapp "github.com/Ricardoleaom/backend-challenge-go/internal/application/wallet"
	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/ledger"
	"github.com/Ricardoleaom/backend-challenge-go/internal/domain/money"
	"github.com/Ricardoleaom/backend-challenge-go/internal/infrastructure/postgres"
)

func requireInternal(w http.ResponseWriter, r *http.Request) bool {
	if !isInternal(r.Context()) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "wallet operations are restricted to the internal service")
		return false
	}
	return true
}

type openWalletRequest struct {
	PlayerID       string   `json:"playerId"`
	InitialBalance moneyDTO `json:"initialBalance"`
}

type walletResponse struct {
	ID       string   `json:"id"`
	PlayerID string   `json:"playerId"`
	Balance  moneyDTO `json:"balance"`
	Version  int64    `json:"version"`
}

func (h *handler) openWallet(w http.ResponseWriter, r *http.Request) {
	if !requireInternal(w, r) {
		return
	}

	var req openWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "malformed JSON body")
		return
	}

	if req.PlayerID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PLAYER_ID", "playerId is required")
		return
	}

	initialBalance, err := req.InitialBalance.toDomain()
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MONEY", err.Error())
		return
	}

	created, err := h.deps.WalletService.Open(r.Context(), walletapp.OpenInput{
		PlayerID:       req.PlayerID,
		InitialBalance: initialBalance,
	})
	if err != nil {
		if errors.Is(err, walletapp.ErrWalletConflict) {
			writeError(w, http.StatusConflict, "WALLET_CONFLICT", "a wallet already exists for this player and currency")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not open wallet")
		return
	}

	writeJSON(w, http.StatusCreated, walletResponse{
		ID:       created.ID(),
		PlayerID: created.PlayerID(),
		Balance:  toMoneyDTO(created.Balance()),
		Version:  created.Version(),
	})
}

func (h *handler) getWallet(w http.ResponseWriter, r *http.Request) {
	if !requireInternal(w, r) {
		return
	}

	id := r.PathValue("id")
	wlt, err := h.deps.Wallets.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, postgres.ErrWalletNotFound) {
			writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load wallet")
		return
	}

	writeJSON(w, http.StatusOK, walletResponse{
		ID:       wlt.ID(),
		PlayerID: wlt.PlayerID(),
		Balance:  toMoneyDTO(wlt.Balance()),
		Version:  wlt.Version(),
	})
}

type ledgerEntryResponse struct {
	ID            string   `json:"id"`
	TransactionID string   `json:"transactionId"`
	Direction     string   `json:"direction"`
	Amount        moneyDTO `json:"amount"`
	BalanceBefore moneyDTO `json:"balanceBefore"`
	BalanceAfter  moneyDTO `json:"balanceAfter"`
	CreatedAt     string   `json:"createdAt"`
}

type ledgerPageResponse struct {
	Entries    []ledgerEntryResponse `json:"entries"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

func (h *handler) getWalletLedger(w http.ResponseWriter, r *http.Request) {
	if !requireInternal(w, r) {
		return
	}

	walletID := r.PathValue("id")

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 500 {
			writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limit must be a positive integer up to 500")
			return
		}
		limit = parsed
	}

	var afterCreatedAt time.Time
	var afterID string
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var err error
		afterCreatedAt, afterID, err = decodeLedgerCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_CURSOR", "cursor is malformed")
			return
		}
	}

	entries, err := h.deps.Ledger.ListByWallet(r.Context(), walletID, afterCreatedAt, afterID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load ledger")
		return
	}

	resp := ledgerPageResponse{Entries: make([]ledgerEntryResponse, 0, len(entries))}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, ledgerEntryResponse{
			ID:            e.ID(),
			TransactionID: e.TransactionID(),
			Direction:     string(e.Direction()),
			Amount:        toMoneyDTO(e.Amount()),
			BalanceBefore: toMoneyDTO(e.BalanceBefore()),
			BalanceAfter:  toMoneyDTO(e.BalanceAfter()),
			CreatedAt:     e.CreatedAt().Format(time.RFC3339Nano),
		})
	}
	if len(entries) == limit {
		last := entries[len(entries)-1]
		resp.NextCursor = encodeLedgerCursor(last.CreatedAt(), last.ID())
	}

	writeJSON(w, http.StatusOK, resp)
}

func encodeLedgerCursor(createdAt time.Time, id string) string {
	raw := fmt.Sprintf("%s|%s", createdAt.Format(time.RFC3339Nano), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeLedgerCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", errors.New("malformed cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", err
	}
	return createdAt, parts[1], nil
}

type reconciliationResponse struct {
	WalletID          string   `json:"walletId"`
	StoredBalance     moneyDTO `json:"storedBalance"`
	CalculatedBalance moneyDTO `json:"calculatedBalance"`
	Difference        moneyDTO `json:"difference"`
	Consistent        bool     `json:"consistent"`
	CheckedEntries    int      `json:"checkedEntries"`
}

// reconcileWallet rebuilds the balance from the ledger (including the
// opening credit) and compares it against the stored balance. It never
// writes anything.
func (h *handler) reconcileWallet(w http.ResponseWriter, r *http.Request) {
	if !requireInternal(w, r) {
		return
	}

	walletID := r.PathValue("id")

	wlt, err := h.deps.Wallets.GetByID(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, postgres.ErrWalletNotFound) {
			writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load wallet")
		return
	}

	calculated, err := money.Zero(wlt.Balance().Currency())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not reconcile wallet")
		return
	}

	checked := 0
	var afterCreatedAt time.Time
	var afterID string
	for {
		entries, err := h.deps.Ledger.ListByWallet(r.Context(), walletID, afterCreatedAt, afterID, 500)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not read ledger")
			return
		}
		if len(entries) == 0 {
			break
		}

		for _, e := range entries {
			calculated, err = applyEntry(calculated, e)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not reconcile wallet")
				return
			}
			checked++
		}

		last := entries[len(entries)-1]
		afterCreatedAt, afterID = last.CreatedAt(), last.ID()
		if len(entries) < 500 {
			break
		}
	}

	difference, err := wlt.Balance().Subtract(calculated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not compare balances")
		return
	}

	cmp, err := wlt.Balance().Compare(calculated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not compare balances")
		return
	}

	consistent := cmp == 0
	if !consistent {
		h.deps.Metrics.ReconciliationMismatches.Inc()
		slog.Error("wallet reconciliation mismatch",
			"wallet_id", walletID,
			"stored_balance", wlt.Balance().String(),
			"calculated_balance", calculated.String(),
		)
	}

	writeJSON(w, http.StatusOK, reconciliationResponse{
		WalletID:          walletID,
		StoredBalance:     toMoneyDTO(wlt.Balance()),
		CalculatedBalance: toMoneyDTO(calculated),
		Difference:        toMoneyDTO(difference),
		Consistent:        consistent,
		CheckedEntries:    checked,
	})
}

func applyEntry(balance money.Money, e ledger.Entry) (money.Money, error) {
	if e.Direction() == ledger.DirectionCredit {
		return balance.Add(e.Amount())
	}
	return balance.Subtract(e.Amount())
}
