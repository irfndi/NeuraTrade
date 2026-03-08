package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// TradeDetails contains all information for a trade notification
type TradeDetails struct {
	Exchange          string
	Symbol            string
	Side              string
	OrderType         string // market, limit
	MarketType        string // spot, futures
	AllowSpotFallback bool
	Leverage          int // for futures
	Amount            decimal.Decimal
	AmountUSDT        decimal.Decimal
	WalletPercent     float64
	EntryPrice        *decimal.Decimal
	TakeProfit        *decimal.Decimal
	StopLoss          *decimal.Decimal
	TradeType         string // scalping, arbitrage, swing, etc.
	Confidence        float64
	Reasoning         string
	OrderID           string
	IsPaperTrade      bool
	ReduceOnly        bool // Must be true for PlaceRiskReductionOrderWithDetails
}

// NativeOrderExecutor implements ScalpingOrderExecutor using native CCXT service
type NativeOrderExecutor struct {
	ccxtService         ccxt.CCXTService
	apiKey              string
	apiSecret           string
	notificationService *NotificationService
	chatID              string
	walletBalance       float64 // USDT balance for % calculation
}

// NewNativeOrderExecutor creates a new native order executor
func NewNativeOrderExecutor(ccxtService ccxt.CCXTService, apiKey, apiSecret string) *NativeOrderExecutor {
	return &NativeOrderExecutor{
		ccxtService: ccxtService,
		apiKey:      apiKey,
		apiSecret:   apiSecret,
	}
}

// SetNotificationService sets the notification service for trade alerts
func (e *NativeOrderExecutor) SetNotificationService(ns *NotificationService) {
	e.notificationService = ns
}

// SetChatID sets the Telegram chat ID for notifications
func (e *NativeOrderExecutor) SetChatID(chatID string) {
	e.chatID = chatID
}

// SetWalletBalance sets the wallet balance for percentage calculations
func (e *NativeOrderExecutor) SetWalletBalance(balance float64) {
	e.walletBalance = balance
}

// PlaceOrder places an order using the native CCXT service
func (e *NativeOrderExecutor) PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	// For paper trading / testing, simulate order placement
	orderID := fmt.Sprintf("paper-order-%s-%s-%s", exchange, symbol, side)

	fmt.Printf("[NATIVE-ORDER] Placing %s order for %s %s (amount: %s USDT)\n", side, exchange, symbol, amount.String())

	return orderID, nil
}

// PlaceOrderWithDetails places an order with full trade details and sends rich notification
func (e *NativeOrderExecutor) PlaceOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	orderID := fmt.Sprintf("paper-order-%s-%s-%s", details.Exchange, details.Symbol, details.Side)
	if details.IsPaperTrade {
		orderID = fmt.Sprintf("paper-%s", orderID)
	}

	fmt.Printf("[NATIVE-ORDER] Placing %s order for %s %s (amount: %s USDT)\n",
		details.Side, details.Exchange, details.Symbol, details.AmountUSDT.String())

	// Send rich Telegram notification
	chatID := strings.TrimSpace(e.chatID)
	if scopedChatID := strings.TrimSpace(scalpingChatIDFromContext(ctx)); scopedChatID != "" {
		chatID = scopedChatID
	}
	if e.notificationService != nil && chatID != "" {
		msg := e.formatTradeNotification(details, orderID)
		chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
		if err != nil {
			fmt.Printf("[NATIVE-ORDER] Invalid Telegram chat ID %q: %v\n", chatID, err)
			return orderID, nil
		}

		go func() {
			notifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := e.notificationService.sendTelegramMessage(notifyCtx, chatIDInt, msg); err != nil {
				fmt.Printf("[NATIVE-ORDER] Failed to send Telegram notification: %v\n", err)
			}
		}()
	}

	return orderID, nil
}

// formatTradeNotification creates a rich formatted trade message
func (e *NativeOrderExecutor) formatTradeNotification(d TradeDetails, orderID string) string {
	// Emoji based on action
	actionEmoji := "🟢"
	if d.Side == "sell" {
		actionEmoji = "🔴"
	}

	// Trade type emoji
	tradeEmoji := "⚡" // scalping
	switch d.TradeType {
	case "arbitrage":
		tradeEmoji = "🔄"
	case "swing":
		tradeEmoji = "📊"
	}

	// Market type string
	marketStr := "Spot"
	if d.MarketType == "futures" {
		marketStr = fmt.Sprintf("Futures (%dx)", d.Leverage)
	}

	var lines []string

	// Header
	if d.IsPaperTrade {
		lines = append(lines, "⚠️ **PAPER TRADE**")
	} else {
		lines = append(lines, "✅ **TRADE EXECUTED**")
	}
	lines = append(lines, "")

	// Main info
	lines = append(lines, fmt.Sprintf("%s **%s %s**", actionEmoji, strings.ToUpper(d.Side), d.Symbol))
	lines = append(lines, "")

	// Trade details table
	lines = append(lines, "━━━━━━━━━━━━━━━━━━━━━")
	caser := cases.Title(language.English)
	lines = append(lines, fmt.Sprintf("%s Type: %s", tradeEmoji, caser.String(d.TradeType)))
	lines = append(lines, fmt.Sprintf("📍 Market: %s", marketStr))
	lines = append(lines, fmt.Sprintf("🏢 Exchange: %s", caser.String(d.Exchange)))
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

	// Reasoning (truncated)
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

// formatPrice formats a price with appropriate precision
func formatPrice(price decimal.Decimal) string {
	f := price.InexactFloat64()
	if f >= 1000 {
		return fmt.Sprintf("$%.2f", f)
	} else if f >= 1 {
		return fmt.Sprintf("$%.4f", f)
	} else if f >= 0.0001 {
		return fmt.Sprintf("$%.6f", f)
	}
	return fmt.Sprintf("$%.8f", f)
}

// GetOpenOrders gets open orders
func (e *NativeOrderExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}

// GetClosedOrders gets closed orders
func (e *NativeOrderExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}

// CancelOrder cancels an order
func (e *NativeOrderExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	return nil
}

// IsPaperTrading returns true for native executor (paper trading mode)
func (e *NativeOrderExecutor) IsPaperTrading() bool {
	return true
}

var _ ScalpingOrderExecutor = (*NativeOrderExecutor)(nil)

// ExecuteTestTrade executes a test trade for debugging purposes
func (e *NativeOrderExecutor) ExecuteTestTrade(ctx context.Context, exchange, symbol, side string) (string, error) {
	amount := decimal.NewFromFloat(10.0)

	details := TradeDetails{
		Exchange:      exchange,
		Symbol:        symbol,
		Side:          side,
		OrderType:     "market",
		MarketType:    "futures",
		Leverage:      5,
		AmountUSDT:    amount,
		WalletPercent: 1.0,
		TradeType:     "scalping",
		Confidence:    0.85,
		Reasoning:     "Test trade to verify system connectivity",
		IsPaperTrade:  true,
	}

	orderID, err := e.PlaceOrderWithDetails(ctx, details)
	if err != nil {
		return "", err
	}

	fmt.Printf("[NATIVE-ORDER] 🧪 TEST TRADE: %s %s %s (amount: %s USDT)\n", side, exchange, symbol, amount.String())

	return orderID, nil
}
