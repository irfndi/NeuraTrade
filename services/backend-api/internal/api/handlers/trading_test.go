package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradingHandlerPlaceOrderAndReadPositions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTradingHandler()
	r := gin.New()
	r.POST("/trading/place_order", h.PlaceOrder)
	r.GET("/trading/positions", h.ListPositions)
	r.GET("/trading/positions/:position_id", h.GetPosition)

	body := `{"exchange":"binance","symbol":"BTC/USDT","side":"BUY","type":"LIMIT","amount":"0.5","price":"50000"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var placeResp struct {
		Status string `json:"status"`
		Data   struct {
			Order struct {
				OrderID    string `json:"order_id"`
				PositionID string `json:"position_id"`
			} `json:"order"`
			Position struct {
				PositionID string `json:"position_id"`
				Status     string `json:"status"`
			} `json:"position"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &placeResp))
	assert.Equal(t, "success", placeResp.Status)
	require.NotEmpty(t, placeResp.Data.Order.OrderID)
	require.NotEmpty(t, placeResp.Data.Order.PositionID)
	assert.Equal(t, "OPEN", placeResp.Data.Position.Status)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/trading/positions?status=OPEN", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var listResp struct {
		Status string `json:"status"`
		Data   struct {
			Count int `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Equal(t, "success", listResp.Status)
	assert.Equal(t, 1, listResp.Data.Count)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/trading/positions/"+placeResp.Data.Order.PositionID, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestTradingHandlerPlaceOrderValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTradingHandler()
	r := gin.New()
	r.POST("/trading/place_order", h.PlaceOrder)

	body := `{"exchange":"binance","symbol":"BTC/USDT","side":"HOLD","amount":"1"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTradingHandlerCancelOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTradingHandler()
	r := gin.New()
	r.POST("/trading/place_order", h.PlaceOrder)
	r.POST("/trading/cancel_order", h.CancelOrder)
	r.GET("/trading/positions", h.ListPositions)

	w := httptest.NewRecorder()
	placeReq := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(`{"exchange":"binance","symbol":"ETH/USDT","side":"BUY","amount":"1"}`))
	placeReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, placeReq)
	require.Equal(t, http.StatusCreated, w.Code)

	var placeResp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &placeResp))
	data := placeResp["data"].(map[string]any)
	order := data["order"].(map[string]any)
	orderID := order["order_id"].(string)

	w = httptest.NewRecorder()
	cancelBody := `{"order_id":"` + orderID + `"}`
	cancelReq := httptest.NewRequest(http.MethodPost, "/trading/cancel_order", bytes.NewBufferString(cancelBody))
	cancelReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, cancelReq)
	require.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/trading/positions?status=OPEN", nil)
	r.ServeHTTP(w, listReq)
	require.Equal(t, http.StatusOK, w.Code)

	var listResp struct {
		Data struct {
			Count int `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Equal(t, 0, listResp.Data.Count)
}

func TestTradingHandlerLiquidateAndLiquidateAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTradingHandler()
	r := gin.New()
	r.POST("/trading/place_order", h.PlaceOrder)
	r.POST("/trading/liquidate", h.Liquidate)
	r.POST("/trading/liquidate_all", h.LiquidateAll)
	r.GET("/trading/positions", h.ListPositions)

	first := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(`{"exchange":"binance","symbol":"SOL/USDT","side":"BUY","amount":"2"}`))
	firstReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(first, firstReq)
	require.Equal(t, http.StatusCreated, first.Code)

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(`{"exchange":"binance","symbol":"ADA/USDT","side":"BUY","amount":"3"}`))
	secondReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(second, secondReq)
	require.Equal(t, http.StatusCreated, second.Code)

	liquidate := httptest.NewRecorder()
	liquidateReq := httptest.NewRequest(http.MethodPost, "/trading/liquidate", bytes.NewBufferString(`{"symbol":"SOL/USDT"}`))
	liquidateReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(liquidate, liquidateReq)
	require.Equal(t, http.StatusOK, liquidate.Code)

	liquidateAll := httptest.NewRecorder()
	liquidateAllReq := httptest.NewRequest(http.MethodPost, "/trading/liquidate_all", nil)
	r.ServeHTTP(liquidateAll, liquidateAllReq)
	require.Equal(t, http.StatusOK, liquidateAll.Code)

	openPositions := httptest.NewRecorder()
	openReq := httptest.NewRequest(http.MethodGet, "/trading/positions?status=OPEN", nil)
	r.ServeHTTP(openPositions, openReq)
	require.Equal(t, http.StatusOK, openPositions.Code)

	var listResp struct {
		Data struct {
			Count int `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(openPositions.Body.Bytes(), &listResp))
	assert.Equal(t, 0, listResp.Data.Count)
}

func TestTradingHandlerCancelOrderNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewTradingHandler()
	r := gin.New()
	r.POST("/trading/cancel_order", h.CancelOrder)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/trading/cancel_order", bytes.NewBufferString(`{"order_id":"ord-missing"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}
