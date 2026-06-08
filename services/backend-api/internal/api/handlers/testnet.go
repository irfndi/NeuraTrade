package handlers

import (
	"net/http"

	"github.com/irfndi/neuratrade/internal/config"
)

type TestnetHandler struct {
	cfg *config.TestnetConfig
}

func NewTestnetHandler(cfg *config.TestnetConfig) *TestnetHandler {
	return &TestnetHandler{cfg: cfg}
}

func (h *TestnetHandler) Health(w http.ResponseWriter, r *http.Request) {
	status := "disabled"
	if h.cfg != nil && h.cfg.Enabled {
		status = "ok"
	}

	exchange := "binance"
	if h.cfg != nil && h.cfg.Exchange != "" {
		exchange = h.cfg.Exchange
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	mode := "testnet"
	if h.cfg == nil || !h.cfg.Enabled {
		mode = "disabled"
	}

	_, _ = w.Write([]byte(`{"status":"` + status + `","mode":"` + mode + `","exchange":"` + exchange + `"}`))
}
