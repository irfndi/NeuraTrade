package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/app/execution/liveguard"
	"github.com/shopspring/decimal"
)

func newTestGuard(t *testing.T) *liveguard.Guard {
	t.Helper()
	cfg := liveguard.Config{
		Enabled:       true,
		ArmRequired:   true,
		SizeCapPct:    decimal.RequireFromString("0.10"),
		FirstNHold:    3,
		ConfirmPhrase: "test-phrase-12345678",
	}
	return liveguard.New(cfg)
}

func setupRouter(guard *liveguard.Guard) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewLiveGuardHandler(guard)
	g := r.Group("/admin/live-guard")
	g.GET("/status", h.LiveGuardStatus)
	g.GET("/pending", h.LiveGuardPending)
	g.POST("/arm", h.ArmLiveTrading)
	g.POST("/disarm", h.DisarmLiveTrading)
	g.POST("/approve/:intentID", h.ApprovePendingOrder)
	g.POST("/reject/:intentID", h.RejectPendingOrder)
	return r
}

func TestLiveGuardHandler_StatusBeforeArm(t *testing.T) {
	r := setupRouter(newTestGuard(t))
	req := httptest.NewRequest(http.MethodGet, "/admin/live-guard/status", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var st liveguard.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Armed {
		t.Fatal("expected not armed")
	}
}

func TestLiveGuardHandler_ArmRequiresPhrase(t *testing.T) {
	r := setupRouter(newTestGuard(t))
	body := strings.NewReader(`{"operator":"op1","phrase":"wrong","reason":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/live-guard/arm", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestLiveGuardHandler_ArmAndStatus(t *testing.T) {
	r := setupRouter(newTestGuard(t))
	body := strings.NewReader(`{"operator":"op1","phrase":"test-phrase-12345678","reason":"go-live"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/live-guard/arm", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	req2 := httptest.NewRequest(http.MethodGet, "/admin/live-guard/status", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	var st liveguard.Status
	if err := json.Unmarshal(rec2.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Armed {
		t.Fatal("expected armed after Arm")
	}
	if st.ArmedBy != "op1" {
		t.Fatalf("ArmedBy: %s", st.ArmedBy)
	}
}

func TestLiveGuardHandler_ApproveAndRejectFlow(t *testing.T) {
	guard := newTestGuard(t)
	if err := guard.Arm("op1", "test-phrase-12345678", "go", false); err != nil {
		t.Fatal(err)
	}
	r := setupRouter(guard)

	_, err := guard.CheckOrder("intent-a", "chat-1", "s", "BTC/USDT", "buy", "market", decimal.NewFromInt(1), true)
	if err != liveguard.ErrOrderPending {
		t.Fatalf("expected pending, got %v", err)
	}

	approveBody := strings.NewReader(`{"operator":"op2","reason":"approved"}`)
	approveReq := httptest.NewRequest(http.MethodPost, "/admin/live-guard/approve/intent-a", approveBody)
	approveReq.Header.Set("Content-Type", "application/json")
	approveRec := httptest.NewRecorder()
	r.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", approveRec.Code, approveRec.Body.String())
	}

	pendingReq := httptest.NewRequest(http.MethodGet, "/admin/live-guard/pending", nil)
	pendingRec := httptest.NewRecorder()
	r.ServeHTTP(pendingRec, pendingReq)
	if strings.Contains(pendingRec.Body.String(), "intent-a") {
		t.Fatalf("intent-a should be gone from pending: %s", pendingRec.Body.String())
	}

	_, err = guard.CheckOrder("intent-b", "chat-1", "s", "BTC/USDT", "buy", "market", decimal.NewFromInt(1), true)
	if err != liveguard.ErrOrderPending {
		t.Fatalf("expected pending for intent-b, got %v", err)
	}
	rejectBody := strings.NewReader(`{"operator":"op3","reason":"too large"}`)
	rejectReq := httptest.NewRequest(http.MethodPost, "/admin/live-guard/reject/intent-b", rejectBody)
	rejectReq.Header.Set("Content-Type", "application/json")
	rejectRec := httptest.NewRecorder()
	r.ServeHTTP(rejectRec, rejectReq)
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rejectRec.Code, rejectRec.Body.String())
	}
}

func TestLiveGuardHandler_ApproveUnknownReturns404(t *testing.T) {
	r := setupRouter(newTestGuard(t))
	body := strings.NewReader(`{"operator":"op"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/live-guard/approve/nonexistent", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestLiveGuardHandler_DisarmIsIdempotent(t *testing.T) {
	r := setupRouter(newTestGuard(t))
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/admin/live-guard/disarm", strings.NewReader(`{"operator":"op1"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("disarm %d: expected 200, got %d", i, rec.Code)
		}
	}
}

func TestLiveGuardHandler_NilGuardReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewLiveGuardHandler(nil)
	r.GET("/admin/live-guard/status", h.LiveGuardStatus)
	req := httptest.NewRequest(http.MethodGet, "/admin/live-guard/status", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestLiveGuardHandler_ConcurrentArmAttempts(t *testing.T) {
	guard := newTestGuard(t)
	r := setupRouter(guard)
	body := []byte(`{"operator":"op","phrase":"test-phrase-12345678","reason":"race"}`)
	var wg sync.WaitGroup
	results := make([]int, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/admin/live-guard/arm", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			results[idx] = rec.Code
		}(i)
	}
	wg.Wait()
	successes := 0
	for _, c := range results {
		if c == http.StatusOK {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 successful arm, got %d (codes: %v)", successes, results)
	}
}
