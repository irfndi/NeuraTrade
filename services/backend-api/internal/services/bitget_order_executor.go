package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var bitgetMinUSDTNotional = decimal.NewFromFloat(6.0)

// BitgetOrderExecutor executes real orders on Bitget exchange
type BitgetOrderExecutor struct {
	apiKey              string
	apiSecret           string
	passphrase          string
	baseURL             string
	notificationService *NotificationService
	chatID              string
	walletBalance       float64
	httpClient          *http.Client
}

// NewBitgetOrderExecutor creates a new Bitget order executor
func NewBitgetOrderExecutor(apiKey, apiSecret, passphrase string) *BitgetOrderExecutor {
	return &BitgetOrderExecutor{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		passphrase: passphrase,
		baseURL:    "https://api.bitget.com",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetNotificationService sets the notification service
func (e *BitgetOrderExecutor) SetNotificationService(ns *NotificationService) {
	e.notificationService = ns
}

// SetChatID sets the Telegram chat ID
func (e *BitgetOrderExecutor) SetChatID(chatID string) {
	e.chatID = chatID
}

// SetWalletBalance sets the wallet balance for percentage calculations
func (e *BitgetOrderExecutor) SetWalletBalance(balance float64) {
	e.walletBalance = balance
}

// PlaceOrder places a real order on Bitget
func (e *BitgetOrderExecutor) PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	// Convert symbol format: BTC/USDT -> BTCUSDT
	apiSymbol := strings.ReplaceAll(symbol, "/", "")

	// For futures scalping
	return e.placeFuturesOrder(ctx, apiSymbol, side, amount, price)
}

// PlaceOrderWithDetails places an order with full trade details
func (e *BitgetOrderExecutor) PlaceOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[BITGET-ORDER] PANIC recovered: %v\n", r)
		}
	}()

	// Convert symbol format
	apiSymbol := strings.ReplaceAll(details.Symbol, "/", "")

	// Validate inputs
	if details.AmountUSDT.IsZero() || details.AmountUSDT.IsNegative() {
		return "", fmt.Errorf("invalid order amount: %s", details.AmountUSDT.String())
	}
	// Keep risk display consistent with actual executable minimum notional on Bitget futures.
	if details.MarketType == "futures" && details.AmountUSDT.LessThan(bitgetMinUSDTNotional) {
		details.AmountUSDT = bitgetMinUSDTNotional
	}

	fmt.Printf("[BITGET-ORDER] Starting order: %s %s (%.2f USDT)\n", details.Side, apiSymbol, details.AmountUSDT.InexactFloat64())

	var orderID string
	var err error

	// Try futures first. Spot fallback is explicit via AllowSpotFallback.
	if details.MarketType == "futures" {
		orderID, err = e.placeFuturesOrderWithTPSL(ctx, apiSymbol, details)
		if err != nil {
			fmt.Printf("[BITGET-ORDER] Futures order failed: %v\n", err)
			if details.AllowSpotFallback && shouldFallbackToSpot(err) {
				fmt.Printf("[BITGET-ORDER] Symbol %s not available on futures, trying spot...\n", apiSymbol)
				details.MarketType = "spot"
				orderID, err = e.placeSpotOrder(ctx, apiSymbol, details.Side, details.AmountUSDT, details.EntryPrice)
			} else if shouldFallbackToSpot(err) {
				return "", fmt.Errorf("futures-only mode prevented spot fallback for %s: %w", apiSymbol, err)
			}
		}
	} else {
		orderID, err = e.placeSpotOrder(ctx, apiSymbol, details.Side, details.AmountUSDT, details.EntryPrice)
	}

	if err != nil {
		fmt.Printf("[BITGET-ORDER] All order attempts failed: %v\n", err)
		return "", err
	}

	fmt.Printf("[BITGET-ORDER] Order successful: %s\n", orderID)

	// Send rich notification
	if e.notificationService != nil && e.chatID != "" {
		msg := e.formatTradeNotification(details, orderID)
		chatIDInt, _ := strconv.ParseInt(e.chatID, 10, 64)

		go func() {
			notifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := e.notificationService.sendTelegramMessage(notifyCtx, chatIDInt, msg); err != nil {
				fmt.Printf("[BITGET-ORDER] Failed to send Telegram notification: %v\n", err)
			}
		}()
	}

	return orderID, nil
}

// placeFuturesOrder places a futures market order
func (e *BitgetOrderExecutor) placeFuturesOrder(ctx context.Context, symbol, side string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	// Bitget v2 expects side=buy|sell (not open_long/open_short).
	bitgetSide, err := normalizeBitgetFuturesSide(side)
	if err != nil {
		return "", err
	}
	holdSide := "long"
	if bitgetSide == "sell" {
		holdSide = "short"
	}

	if amount.LessThan(bitgetMinUSDTNotional) {
		fmt.Printf("[BITGET-ORDER] Amount %s USDT below minimum, bumping to %s USDT\n",
			amount.String(), bitgetMinUSDTNotional.String())
		amount = bitgetMinUSDTNotional
	}

	// For buy, we're opening a long position
	// Calculate size in contracts (for USDT-FUTURES, size is in USDT)
	size := amount.String()

	body := map[string]interface{}{
		"symbol":      symbol,
		"productType": "USDT-FUTURES",
		"marginMode":  "crossed",
		"marginCoin":  "USDT",
		"size":        size,
		"side":        bitgetSide,
		"tradeSide":   "open",
		"holdSide":    holdSide,
		"orderType":   "market",
	}

	result, err := e.placeBitgetFuturesOrderRequest(ctx, body)
	if err != nil {
		return "", fmt.Errorf("failed to place futures order: %w", err)
	}
	if result.Code == "400172" {
		legacyBody := cloneStringAnyMap(body)
		legacyBody["side"] = legacyBitgetOpenSide(bitgetSide)
		delete(legacyBody, "tradeSide")
		delete(legacyBody, "holdSide")
		result, err = e.placeBitgetFuturesOrderRequest(ctx, legacyBody)
		if err != nil {
			return "", fmt.Errorf("failed to place futures order (legacy side): %w", err)
		}
	}

	if result.Code != "00000" {
		return "", fmt.Errorf("bitget API error: %s (code: %s)", result.Msg, result.Code)
	}

	fmt.Printf("[BITGET-ORDER] ✅ Futures order placed: %s %s (size: %s USDT) - OrderID: %s\n",
		side, symbol, size, result.OrderID)

	return result.OrderID, nil
}

// placeFuturesOrderWithTPSL places a futures order with TP/SL
func (e *BitgetOrderExecutor) placeFuturesOrderWithTPSL(ctx context.Context, symbol string, details TradeDetails) (string, error) {
	bitgetSide, err := normalizeBitgetFuturesSide(details.Side)
	if err != nil {
		return "", err
	}
	isRiskReduction := strings.EqualFold(strings.TrimSpace(details.TradeType), "risk_reduction")
	tradeSide := "open"
	holdSide := "long"
	if bitgetSide == "sell" {
		holdSide = "short"
	}
	if isRiskReduction {
		tradeSide = "close"
		// To close longs, side=sell and holdSide=long. To close shorts, side=buy and holdSide=short.
		if bitgetSide == "sell" {
			holdSide = "long"
		} else {
			holdSide = "short"
		}
	}

	// Get current price to calculate contract size
	var price decimal.Decimal
	if details.EntryPrice != nil {
		price = *details.EntryPrice
	} else {
		// Fetch current price from Bitget
		ticker, err := e.getTicker(ctx, symbol)
		if err != nil {
			return "", fmt.Errorf("failed to get ticker: %w", err)
		}
		price = ticker
	}

	if price.IsZero() {
		return "", fmt.Errorf("cannot place order with zero price")
	}

	// Get contract info to determine size precision/multiplier
	contractInfo, err := e.getContractInfo(ctx, symbol)
	if err != nil {
		fmt.Printf("[BITGET-ORDER] Failed to get contract info for %s: %v\n", symbol, err)
		return "", fmt.Errorf("failed to get contract info: %w", err)
	}

	var contractSize decimal.Decimal
	if isRiskReduction {
		contractSize = details.Amount.Abs()
		if contractSize.LessThanOrEqual(decimal.Zero) {
			return "", fmt.Errorf("risk reduction order requires positive contract amount")
		}
		contractSize = contractSize.RoundFloor(safeInt32(contractInfo.VolumePlace))
		if contractSize.LessThan(contractInfo.MinTradeNum) {
			return "", fmt.Errorf(
				"risk reduction amount %s is below minimum tradable size %s for %s",
				contractSize.String(),
				contractInfo.MinTradeNum.String(),
				symbol,
			)
		}
	} else {
		amountUSDT := details.AmountUSDT
		if amountUSDT.LessThan(bitgetMinUSDTNotional) {
			fmt.Printf("[BITGET-ORDER] Amount %s USDT below minimum, bumping to %s USDT\n",
				amountUSDT.String(), bitgetMinUSDTNotional.String())
			amountUSDT = bitgetMinUSDTNotional
		}

		// Calculate contract size:
		// For USDT-FUTURES: size = (USDT amount / price) / sizeMultiplier
		baseAmount := amountUSDT.Div(price)
		if contractInfo.SizeMultiplier.LessThanOrEqual(decimal.Zero) {
			return "", fmt.Errorf("invalid contract size multiplier for %s", symbol)
		}
		contractSize = baseAmount.Div(contractInfo.SizeMultiplier)
		// Round up so post-rounding notional does not slip below exchange minimum.
		contractSize = contractSize.RoundCeil(safeInt32(contractInfo.VolumePlace))
		// Ensure minimum size
		if contractSize.LessThan(contractInfo.MinTradeNum) {
			contractSize = contractInfo.MinTradeNum
		}
		// Ensure resulting notional is not below Bitget's minimum without iterative bumps.
		minContractsForNotional := bitgetMinUSDTNotional.
			Div(price).
			Div(contractInfo.SizeMultiplier).
			RoundCeil(safeInt32(contractInfo.VolumePlace))
		if contractSize.LessThan(minContractsForNotional) {
			contractSize = minContractsForNotional
		}
	}
	size := contractSize.String()

	if isRiskReduction {
		fmt.Printf("[BITGET-ORDER] Risk reduction size calc: requested=%s contracts rounded=%s\n",
			details.Amount.Abs().String(), size)
	} else {
		baseAmount := details.AmountUSDT.Div(price)
		fmt.Printf("[BITGET-ORDER] Size calc: %.2f USDT / %s = %.2f base / %s = %s contracts\n",
			details.AmountUSDT.InexactFloat64(), price.StringFixed(5), baseAmount.InexactFloat64(),
			contractInfo.SizeMultiplier.String(), size)
	}

	body := map[string]interface{}{
		"symbol":      symbol,
		"productType": "USDT-FUTURES",
		"marginMode":  "isolated",
		"marginCoin":  "USDT",
		"size":        size,
		"side":        bitgetSide,
		"tradeSide":   tradeSide,
		"holdSide":    holdSide,
		"orderType":   "market",
	}
	if isRiskReduction {
		body["reduceOnly"] = "YES"
	}

	// Add TP/SL if provided
	if !isRiskReduction && details.TakeProfit != nil {
		body["presetTakeProfitPrice"] = formatFuturesTriggerPrice(*details.TakeProfit, contractInfo)
	}
	if !isRiskReduction && details.StopLoss != nil {
		body["presetStopLossPrice"] = formatFuturesTriggerPrice(*details.StopLoss, contractInfo)
	}

	fmt.Printf("[BITGET-ORDER] Placing futures order: %s %s (tradeSide=%s holdSide=%s size=%s @ %s)\n",
		bitgetSide, symbol, tradeSide, holdSide, size, price.StringFixed(5))
	fmt.Printf("[BITGET-ORDER] Futures payload: %s\n", marshalForLog(body))

	result, err := e.placeBitgetFuturesOrderRequest(ctx, body)
	if err != nil {
		return "", fmt.Errorf("failed to place futures order: %w", err)
	}
	if !isRiskReduction && result.Code == "400172" {
		legacyBody := cloneStringAnyMap(body)
		legacyBody["side"] = legacyBitgetOpenSide(bitgetSide)
		delete(legacyBody, "tradeSide")
		delete(legacyBody, "holdSide")
		delete(legacyBody, "reduceOnly")
		result, err = e.placeBitgetFuturesOrderRequest(ctx, legacyBody)
		if err != nil {
			return "", fmt.Errorf("failed to place futures order (legacy side): %w", err)
		}
	}
	if result.Code != "00000" {
		return "", fmt.Errorf("bitget API error: %s (code: %s)", result.Msg, result.Code)
	}

	fmt.Printf("[BITGET-ORDER] ✅ Futures order placed: %s %s (size: %s contracts) - OrderID: %s\n",
		details.Side, symbol, size, result.OrderID)

	return result.OrderID, nil
}

// getTicker fetches current ticker price from Bitget
func (e *BitgetOrderExecutor) getTicker(ctx context.Context, symbol string) (decimal.Decimal, error) {
	endpoint := fmt.Sprintf("/api/v2/mix/market/ticker?productType=USDT-FUTURES&symbol=%s", symbol)
	resp, err := e.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to get ticker: %w", err)
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			LastPr string `json:"lastPr"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return decimal.Zero, fmt.Errorf("failed to parse ticker response: %w", err)
	}

	if result.Code != "00000" {
		return decimal.Zero, fmt.Errorf("bitget ticker API error: %s (code: %s)", result.Msg, result.Code)
	}

	if len(result.Data) == 0 {
		return decimal.Zero, fmt.Errorf("no ticker data for symbol %s", symbol)
	}

	return decimal.RequireFromString(result.Data[0].LastPr), nil
}

// ContractInfo holds contract specification info
type ContractInfo struct {
	SizeMultiplier decimal.Decimal
	MinTradeNum    decimal.Decimal
	VolumePlace    int
	PricePlace     int
}

// getContractInfo fetches contract specification from Bitget
func (e *BitgetOrderExecutor) getContractInfo(ctx context.Context, symbol string) (*ContractInfo, error) {
	endpoint := fmt.Sprintf("/api/v2/mix/market/contracts?productType=USDT-FUTURES&symbol=%s", symbol)
	resp, err := e.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get contract info: %w", err)
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			SizeMultiplier string `json:"sizeMultiplier"`
			MinTradeNum    string `json:"minTradeNum"`
			VolumePlace    string `json:"volumePlace"`
			PricePlace     string `json:"pricePlace"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse contract response: %w", err)
	}

	if result.Code != "00000" || len(result.Data) == 0 {
		return nil, fmt.Errorf("bitget contract API error: %s", result.Msg)
	}

	info := &ContractInfo{
		SizeMultiplier: decimal.RequireFromString(result.Data[0].SizeMultiplier),
		MinTradeNum:    decimal.RequireFromString(result.Data[0].MinTradeNum),
	}
	if vp, err := strconv.Atoi(result.Data[0].VolumePlace); err == nil {
		info.VolumePlace = vp
	}
	if pp, err := strconv.Atoi(result.Data[0].PricePlace); err == nil {
		info.PricePlace = pp
	}

	return info, nil
}

func formatFuturesTriggerPrice(price decimal.Decimal, contractInfo *ContractInfo) string {
	if contractInfo == nil {
		return price.StringFixed(5)
	}
	if contractInfo.PricePlace <= 0 {
		return price.Round(0).String()
	}
	scale := safeInt32(contractInfo.PricePlace)
	return price.Round(scale).StringFixed(scale)
}

func safeInt32(value int) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value < math.MinInt32 {
		return math.MinInt32
	}
	return int32(value)
}

func shouldFallbackToSpot(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "does not exist") ||
		strings.Contains(errMsg, "removed") ||
		strings.Contains(errMsg, "failed to get ticker") ||
		strings.Contains(errMsg, "failed to get contract")
}

func normalizeBitgetFuturesSide(side string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(side))
	switch normalized {
	case "buy", "long", "open_long":
		return "buy", nil
	case "sell", "short", "open_short":
		return "sell", nil
	default:
		return "", fmt.Errorf("unsupported futures side %q", side)
	}
}

type bitgetOrderResult struct {
	Code    string
	Msg     string
	OrderID string
}

func (e *BitgetOrderExecutor) placeBitgetFuturesOrderRequest(ctx context.Context, body map[string]interface{}) (*bitgetOrderResult, error) {
	jsonBody, _ := json.Marshal(body)
	fmt.Printf("[BITGET-ORDER] Futures payload: %s\n", string(jsonBody))

	resp, err := e.doRequest(ctx, "POST", "/api/v2/mix/order/place-order", jsonBody)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OrderID string `json:"orderId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &bitgetOrderResult{
		Code:    parsed.Code,
		Msg:     parsed.Msg,
		OrderID: parsed.Data.OrderID,
	}, nil
}

func legacyBitgetOpenSide(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "buy":
		return "open_long"
	case "sell":
		return "open_short"
	default:
		return side
	}
}

func cloneStringAnyMap(input map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(input))
	for k, v := range input {
		cloned[k] = v
	}
	return cloned
}

// placeSpotOrder places a spot market order
func (e *BitgetOrderExecutor) placeSpotOrder(ctx context.Context, symbol, side string, amountUSDT decimal.Decimal, price *decimal.Decimal) (string, error) {
	// Get current price if not provided
	var currentPrice decimal.Decimal
	if price != nil && !price.IsZero() {
		currentPrice = *price
	} else {
		// Fetch current price from Bitget spot
		ticker, err := e.getSpotTicker(ctx, symbol)
		if err != nil {
			return "", fmt.Errorf("failed to get spot ticker: %w", err)
		}
		currentPrice = ticker
	}

	if currentPrice.IsZero() {
		return "", fmt.Errorf("cannot place order with zero price")
	}

	// Calculate size in base currency: size = USDT amount / price
	// For SONIC/USDT: 3 USDT / 0.04 = 75 SONIC
	baseSize := amountUSDT.Div(currentPrice).Round(2) // Round to 2 decimal places

	body := map[string]interface{}{
		"symbol":    symbol,
		"side":      side,
		"orderType": "market",
		"size":      baseSize.String(), // Base currency amount
	}

	jsonBody, _ := json.Marshal(body)

	fmt.Printf("[BITGET-ORDER] Placing spot order: %s %s (%s base = %.2f USDT @ %s)\n",
		side, symbol, baseSize.String(), amountUSDT.InexactFloat64(), currentPrice.StringFixed(5))

	resp, err := e.doRequest(ctx, "POST", "/api/v2/spot/trade/place-order", jsonBody)
	if err != nil {
		return "", fmt.Errorf("failed to place spot order: %w", err)
	}

	fmt.Printf("[BITGET-ORDER] API Response: %s\n", string(resp))

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OrderID string `json:"orderId"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Code != "00000" {
		return "", fmt.Errorf("bitget API error: %s (code: %s)", result.Msg, result.Code)
	}

	fmt.Printf("[BITGET-ORDER] ✅ Spot order placed: %s %s (%s base) - OrderID: %s\n",
		side, symbol, baseSize.String(), result.Data.OrderID)

	return result.Data.OrderID, nil
}

// getSpotTicker fetches current spot ticker price from Bitget
func (e *BitgetOrderExecutor) getSpotTicker(ctx context.Context, symbol string) (decimal.Decimal, error) {
	endpoint := fmt.Sprintf("/api/v2/spot/market/tickers?symbol=%s", symbol)
	resp, err := e.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to get spot ticker: %w", err)
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			LastPr string `json:"lastPr"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return decimal.Zero, fmt.Errorf("failed to parse spot ticker response: %w", err)
	}

	if result.Code != "00000" || len(result.Data) == 0 {
		return decimal.Zero, fmt.Errorf("bitget spot ticker API error: %s", result.Msg)
	}

	return decimal.RequireFromString(result.Data[0].LastPr), nil
}

// doRequest makes an authenticated request to Bitget API
func (e *BitgetOrderExecutor) doRequest(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())

	// Build signature string: timestamp + method + endpoint + body
	signStr := timestamp + method + endpoint
	if body != nil {
		signStr += string(body)
	}

	// Generate signature
	signature := e.sign(signStr)

	// Build request
	url := e.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ACCESS-KEY", e.apiKey)
	req.Header.Set("ACCESS-SIGN", signature)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("ACCESS-PASSPHRASE", e.passphrase)
	req.Header.Set("locale", "en-US")

	// Execute request
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return respBody, nil
}

// sign generates HMAC-SHA256 signature for Bitget API
func (e *BitgetOrderExecutor) sign(message string) string {
	key := []byte(e.apiSecret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// formatTradeNotification creates a rich formatted trade message
func (e *BitgetOrderExecutor) formatTradeNotification(d TradeDetails, orderID string) string {
	actionEmoji := "🟢"
	if d.Side == "sell" {
		actionEmoji = "🔴"
	}

	tradeEmoji := "⚡"
	switch d.TradeType {
	case "arbitrage":
		tradeEmoji = "🔄"
	case "swing":
		tradeEmoji = "📊"
	}

	marketStr := "Spot"
	if d.MarketType == "futures" {
		marketStr = fmt.Sprintf("Futures (%dx)", d.Leverage)
	}

	var lines []string

	// Header - REAL TRADE
	lines = append(lines, "✅ **TRADE EXECUTED**")
	lines = append(lines, "")

	// Main info
	lines = append(lines, fmt.Sprintf("%s **%s %s**", actionEmoji, strings.ToUpper(d.Side), d.Symbol))
	lines = append(lines, "")

	// Trade details table
	lines = append(lines, "━━━━━━━━━━━━━━━━━━━━━")
	caser := cases.Title(language.English)
	lines = append(lines, fmt.Sprintf("%s Type: %s", tradeEmoji, caser.String(d.TradeType)))
	lines = append(lines, fmt.Sprintf("📍 Market: %s", marketStr))
	lines = append(lines, "🏢 Exchange: Bitget")
	lines = append(lines, "")

	// Position size
	lines = append(lines, "💰 **Position Size**")
	lines = append(lines, fmt.Sprintf("   Amount: %.2f USDT", d.AmountUSDT.InexactFloat64()))
	if d.WalletPercent > 0 {
		lines = append(lines, fmt.Sprintf("   Wallet: %.1f%%", d.WalletPercent))
	}
	if d.EntryPrice != nil {
		lines = append(lines, fmt.Sprintf("   Entry: %s", formatPrice(*d.EntryPrice)))
	}
	lines = append(lines, "")

	// Risk management
	if d.TakeProfit != nil || d.StopLoss != nil {
		lines = append(lines, "🎯 **Risk Management**")
		if d.TakeProfit != nil {
			tpPercent := 0.0
			if d.EntryPrice != nil && !d.EntryPrice.IsZero() {
				tpPercent = d.TakeProfit.Sub(*d.EntryPrice).Div(*d.EntryPrice).Mul(decimal.NewFromInt(100)).InexactFloat64()
			}
			lines = append(lines, fmt.Sprintf("   🟢 TP: %s (%.1f%%)", formatPrice(*d.TakeProfit), tpPercent))
		}
		if d.StopLoss != nil {
			slPercent := 0.0
			if d.EntryPrice != nil && !d.EntryPrice.IsZero() {
				slPercent = d.StopLoss.Sub(*d.EntryPrice).Div(*d.EntryPrice).Mul(decimal.NewFromInt(100)).InexactFloat64()
			}
			lines = append(lines, fmt.Sprintf("   🔴 SL: %s (%.1f%%)", formatPrice(*d.StopLoss), slPercent))
		}
		lines = append(lines, "")
	}

	// AI Confidence
	if d.Confidence > 0 {
		confEmoji := "📊"
		if d.Confidence >= 0.7 {
			confEmoji = "🔥"
		} else if d.Confidence >= 0.5 {
			confEmoji = "✨"
		}
		lines = append(lines, fmt.Sprintf("%s Confidence: %.0f%%", confEmoji, d.Confidence*100))
	}

	// Reasoning
	if d.Reasoning != "" {
		maxLen := 150
		reasoning := d.Reasoning
		if len(reasoning) > maxLen {
			reasoning = reasoning[:maxLen] + "..."
		}
		lines = append(lines, fmt.Sprintf("💭 _%s_", reasoning))
	}

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("🎫 Order: `%s`", orderID))

	return strings.Join(lines, "\n")
}

// GetOpenOrders gets open orders
func (e *BitgetOrderExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	apiSymbol := strings.ReplaceAll(symbol, "/", "")

	endpoint := fmt.Sprintf("/api/v2/mix/order/orders-pending?productType=USDT-FUTURES&symbol=%s", apiSymbol)
	resp, err := e.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	return parseBitgetOrderList(result.Data)
}

// GetClosedOrders gets closed orders
func (e *BitgetOrderExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	apiSymbol := strings.ReplaceAll(symbol, "/", "")

	endpoint := fmt.Sprintf("/api/v2/mix/order/orders-history?productType=USDT-FUTURES&symbol=%s&limit=%d", apiSymbol, limit)
	resp, err := e.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	return parseBitgetOrderList(result.Data)
}

// CancelOrder cancels an order
func (e *BitgetOrderExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	body := map[string]interface{}{
		"orderId":     orderID,
		"productType": "USDT-FUTURES",
	}

	jsonBody, _ := json.Marshal(body)

	resp, err := e.doRequest(ctx, "POST", "/api/v2/mix/order/cancel-order", jsonBody)
	if err != nil {
		return err
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return err
	}

	if result.Code != "00000" {
		return fmt.Errorf("failed to cancel order: %s", result.Msg)
	}

	return nil
}

func (e *BitgetOrderExecutor) SyncPositionProtection(
	ctx context.Context,
	exchange string,
	position ManagedOpenPosition,
	stopLoss decimal.Decimal,
	takeProfit decimal.Decimal,
) error {
	if !strings.EqualFold(strings.TrimSpace(exchange), "bitget") {
		return ErrProtectionSyncUnsupported
	}
	apiSymbol := strings.ReplaceAll(strings.TrimSpace(position.Symbol), "/", "")
	if apiSymbol == "" {
		return fmt.Errorf("symbol is required for exchange-side protection sync")
	}
	holdSide := "short"
	if isLongSide(position.Side) {
		holdSide = "long"
	}

	contractInfo, err := e.getContractInfo(ctx, apiSymbol)
	if err != nil {
		return fmt.Errorf("fetch contract info for protection sync: %w", err)
	}
	if err := e.cancelExistingPositionTPSL(ctx, apiSymbol, holdSide); err != nil {
		return fmt.Errorf("cancel existing TP/SL plan orders failed: %w", err)
	}
	if stopLoss.LessThanOrEqual(decimal.Zero) && takeProfit.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	if err := e.placePositionTPSL(ctx, apiSymbol, holdSide, stopLoss, takeProfit, contractInfo); err != nil {
		return fmt.Errorf("place replacement TP/SL plan order failed: %w", err)
	}
	return nil
}

func (e *BitgetOrderExecutor) cancelExistingPositionTPSL(ctx context.Context, symbol, holdSide string) error {
	endpoint := fmt.Sprintf(
		"/api/v2/mix/order/orders-plan-pending?productType=USDT-FUTURES&planType=profit_loss&symbol=%s",
		symbol,
	)
	resp, err := e.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("list pending TP/SL plans: %w", err)
	}

	var result struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("failed to parse pending TP/SL orders response: %w", err)
	}
	if result.Code != "00000" {
		return fmt.Errorf("bitget API error while listing TP/SL plans: %s (code: %s)", result.Msg, result.Code)
	}

	orders, err := parseBitgetOrderList(result.Data)
	if err != nil {
		return fmt.Errorf("failed to parse pending TP/SL orders payload: %w", err)
	}
	if len(orders) == 0 {
		return nil
	}

	orderIDList := make([]map[string]string, 0, len(orders))
	for _, order := range orders {
		orderID := strings.TrimSpace(mapStringAny(order, "orderId", "orderID", "id"))
		if orderID == "" {
			continue
		}
		recordHoldSide := strings.ToLower(strings.TrimSpace(mapStringAny(order, "holdSide", "posSide")))
		if recordHoldSide != "" && recordHoldSide != strings.ToLower(strings.TrimSpace(holdSide)) {
			continue
		}
		orderIDList = append(orderIDList, map[string]string{"orderId": orderID})
	}
	if len(orderIDList) == 0 {
		return nil
	}

	cancelBody := map[string]interface{}{
		"symbol":      symbol,
		"productType": "USDT-FUTURES",
		"marginCoin":  "USDT",
		"orderIdList": orderIDList,
	}
	jsonBody, err := json.Marshal(cancelBody)
	if err != nil {
		return fmt.Errorf("marshal cancel TP/SL payload: %w", err)
	}
	cancelResp, err := e.doRequest(ctx, "POST", "/api/v2/mix/order/cancel-plan-order", jsonBody)
	if err != nil {
		return fmt.Errorf("cancel TP/SL plans request: %w", err)
	}

	var cancelResult struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(cancelResp, &cancelResult); err != nil {
		return fmt.Errorf("failed to parse cancel TP/SL response: %w", err)
	}
	if cancelResult.Code != "00000" {
		return fmt.Errorf("bitget cancel TP/SL error: %s (code: %s)", cancelResult.Msg, cancelResult.Code)
	}
	return nil
}

func (e *BitgetOrderExecutor) placePositionTPSL(
	ctx context.Context,
	symbol, holdSide string,
	stopLoss decimal.Decimal,
	takeProfit decimal.Decimal,
	contractInfo *ContractInfo,
) error {
	body := map[string]interface{}{
		"symbol":      symbol,
		"productType": "USDT-FUTURES",
		"marginCoin":  "USDT",
		"holdSide":    holdSide,
	}

	if takeProfit.GreaterThan(decimal.Zero) {
		body["stopSurplusTriggerPrice"] = formatFuturesTriggerPrice(takeProfit, contractInfo)
		body["stopSurplusTriggerType"] = "mark_price"
		body["stopSurplusExecutePrice"] = "0"
	}
	if stopLoss.GreaterThan(decimal.Zero) {
		body["stopLossTriggerPrice"] = formatFuturesTriggerPrice(stopLoss, contractInfo)
		body["stopLossTriggerType"] = "mark_price"
		body["stopLossExecutePrice"] = "0"
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal place TP/SL payload: %w", err)
	}
	resp, err := e.doRequest(ctx, "POST", "/api/v2/mix/order/place-pos-tpsl", jsonBody)
	if err != nil {
		return fmt.Errorf("place TP/SL request: %w", err)
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("failed to parse place TP/SL response: %w", err)
	}
	if result.Code != "00000" {
		return fmt.Errorf("bitget place TP/SL error: %s (code: %s)", result.Msg, result.Code)
	}
	return nil
}

// IsPaperTrading returns false for Bitget executor (real trading mode)
func (e *BitgetOrderExecutor) IsPaperTrading() bool {
	return false
}

var _ ScalpingOrderExecutor = (*BitgetOrderExecutor)(nil)

func marshalForLog(v interface{}) string {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(payload)
}

func mapStringAny(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return typed
			}
		case fmt.Stringer:
			text := typed.String()
			if strings.TrimSpace(text) != "" {
				return text
			}
		default:
			text := fmt.Sprintf("%v", typed)
			if strings.TrimSpace(text) != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func parseBitgetOrderList(raw json.RawMessage) ([]map[string]interface{}, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var direct []map[string]interface{}
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}

	var container map[string]interface{}
	if err := json.Unmarshal(raw, &container); err != nil {
		return nil, err
	}

	knownListKeys := []string{
		"entrustedList",
		"orderList",
		"list",
		"rows",
	}
	for _, key := range knownListKeys {
		value, ok := container[key]
		if !ok {
			continue
		}
		items, ok := value.([]interface{})
		if !ok {
			continue
		}
		result := make([]map[string]interface{}, 0, len(items))
		for _, item := range items {
			record, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			result = append(result, record)
		}
		return result, nil
	}

	return nil, nil
}
