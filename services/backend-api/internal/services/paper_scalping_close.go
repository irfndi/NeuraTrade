package services

import (
	"context"
	"fmt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"math/rand"
	"os"
	"strings"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/shopspring/decimal"
)

const (
	scalpingPaperTakeProfitCloseSource = "autonomous_scalping_paper_take_profit"
	scalpingPaperStopLossCloseSource   = "autonomous_scalping_paper_stop_loss"
	defaultPaperScalpingCloseLimit     = 100
)

var defaultPaperScalpingTakerFeeRate = decimal.RequireFromString("0.0006")

func (h *IntegratedQuestHandlers) closeTriggeredPaperScalpingPositions(
	ctx context.Context,
	quest *Quest,
	chatID, exchange string,
) (int, error) {
	if h == nil || h.lifecycleStore == nil {
		return 0, nil
	}
	positions, err := h.lifecycleStore.ListManagedOpenPositions(ctx, chatID, exchange, paperScalpingClosePositionLimit())
	if err != nil {
		return 0, fmt.Errorf("list paper scalping positions: %w", err)
	}
	if len(positions) == 0 {
		return 0, nil
	}
	if quest != nil && quest.Checkpoint == nil {
		quest.Checkpoint = make(map[string]interface{})
	}

	closed := 0
	evaluated := 0
	missingPrices := 0
	for _, position := range positions {
		if !strings.EqualFold(strings.TrimSpace(position.Source), scalpingPaperLifecycleSource) {
			continue
		}
		evaluated++
		exitPrice, ok := h.resolvePaperScalpingMarkPrice(ctx, position)
		if !ok {
			missingPrices++
			continue
		}
		triggerSource := paperScalpingCloseTrigger(position, exitPrice)
		if triggerSource == "" {
			continue
		}

		baseFilled := paperScalpingBaseFilled(position)
		if baseFilled.LessThanOrEqual(decimal.Zero) {
			baseFilled = position.Size.Abs()
		}
		grossPnL := paperScalpingGrossPnL(position.Side, position.EntryPrice, exitPrice, baseFilled)
		fees := paperScalpingRoundTripFees(position.EntryPrice, exitPrice, baseFilled)
		netPnL := adjustedLifecyclePnL(grossPnL, fees, position.Exchange, triggerSource)
		outcome := paperScalpingOutcomeFromNetPnL(netPnL)
		orderID := strings.TrimSpace(position.OrderID)
		if orderID == "" {
			orderID = strings.TrimSpace(position.PositionID)
		}
		if orderID == "" {
			return closed, fmt.Errorf("paper scalping position %s has no order id", position.PositionID)
		}
		closedAt := time.Now().UTC()

		if err := h.lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
			OrderID:     orderID,
			ChatID:      position.ChatID,
			Exchange:    position.Exchange,
			Symbol:      position.Symbol,
			Side:        position.Side,
			MarketType:  position.MarketType,
			Filled:      baseFilled,
			EntryPrice:  position.EntryPrice,
			ExitPrice:   exitPrice,
			RealizedPnL: grossPnL,
			Fees:        fees,
			Source:      triggerSource,
			ClosedAt:    closedAt,
		}); err != nil {
			return closed, fmt.Errorf("record paper scalping close for %s: %w", orderID, err)
		}

		h.updatePaperScalpingTelemetryOutcome(ctx, orderID, outcome, netPnL, position.OpenedAt, closedAt)
		closed++
		zaplogrus.Infof(
			"[SCALPING] Paper %s closed by %s at %s (entry=%s gross_pnl=%s fees=%s)",
			position.Symbol,
			triggerSource,
			exitPrice.String(),
			position.EntryPrice.String(),
			grossPnL.String(),
			fees.String(),
		)
	}

	if quest != nil && quest.Checkpoint != nil {
		quest.Checkpoint["paper_close_positions_evaluated"] = evaluated
		if closed > 0 {
			quest.Checkpoint["paper_close_triggered_positions"] = closed
		}
		if missingPrices > 0 {
			quest.Checkpoint["paper_close_missing_prices"] = missingPrices
		}
	}

	return closed, nil
}

func (h *IntegratedQuestHandlers) resolvePaperScalpingMarkPrice(
	ctx context.Context,
	position ManagedOpenPosition,
) (decimal.Decimal, bool) {
	var rawPrice decimal.Decimal
	if h != nil {
		tickerSource, ok := h.ccxtService.(interface {
			FetchSingleTicker(ctx context.Context, exchange, symbol string) (ccxt.MarketPriceInterface, error)
		})
		if ok {
			ticker, err := tickerSource.FetchSingleTicker(ctx, position.Exchange, position.Symbol)
			if err == nil && ticker != nil && ticker.GetPrice() > 0 {
				rawPrice = decimal.NewFromFloat(ticker.GetPrice())
			}
		}
	}
	if rawPrice.LessThanOrEqual(decimal.Zero) && position.LastPrice.GreaterThan(decimal.Zero) {
		rawPrice = position.LastPrice
	}
	if rawPrice.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, false
	}
	// Apply realistic slippage simulation (±0.02% to ±0.10% based on position size)
	// Tests can set NEURATRADE_PAPER_SCALPING_SLIPPAGE_BPS=0 to disable slippage.
	slipBps := decimal.NewFromFloat(0.0002) // base 2 bps
	if position.Size.GreaterThan(decimal.NewFromInt(50)) {
		slipBps = decimal.NewFromFloat(0.0010) // 10 bps for large positions
	} else if position.Size.GreaterThan(decimal.NewFromInt(10)) {
		slipBps = decimal.NewFromFloat(0.0005) // 5 bps for medium positions
	}
	if v := os.Getenv("NEURATRADE_PAPER_SCALPING_SLIPPAGE_BPS"); v != "" {
		if parsed, err := decimal.NewFromString(v); err == nil {
			slipBps = parsed
		}
	}
	slipFactor := decimal.NewFromFloat(1.0 + (rand.Float64()*2.0-1.0)*slipBps.InexactFloat64())
	return rawPrice.Mul(slipFactor).Round(8), true
}

func paperScalpingCloseTrigger(position ManagedOpenPosition, exitPrice decimal.Decimal) string {
	if exitPrice.LessThanOrEqual(decimal.Zero) || position.EntryPrice.LessThanOrEqual(decimal.Zero) {
		return ""
	}
	side := normalizeLifecycleSide(position.Side)
	switch side {
	case "buy":
		if position.TakeProfit.GreaterThan(decimal.Zero) && exitPrice.GreaterThanOrEqual(position.TakeProfit) {
			return scalpingPaperTakeProfitCloseSource
		}
		if position.StopLoss.GreaterThan(decimal.Zero) && exitPrice.LessThanOrEqual(position.StopLoss) {
			return scalpingPaperStopLossCloseSource
		}
	case "sell":
		if position.TakeProfit.GreaterThan(decimal.Zero) && exitPrice.LessThanOrEqual(position.TakeProfit) {
			return scalpingPaperTakeProfitCloseSource
		}
		if position.StopLoss.GreaterThan(decimal.Zero) && exitPrice.GreaterThanOrEqual(position.StopLoss) {
			return scalpingPaperStopLossCloseSource
		}
	}
	return ""
}

func paperScalpingBaseFilled(position ManagedOpenPosition) decimal.Decimal {
	if position.EntryPrice.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	return position.Size.Abs().Div(position.EntryPrice)
}

func paperScalpingGrossPnL(side string, entryPrice, exitPrice, baseFilled decimal.Decimal) decimal.Decimal {
	if entryPrice.LessThanOrEqual(decimal.Zero) || exitPrice.LessThanOrEqual(decimal.Zero) || baseFilled.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	delta := exitPrice.Sub(entryPrice)
	if normalizeLifecycleSide(side) == "sell" {
		delta = entryPrice.Sub(exitPrice)
	}
	return delta.Mul(baseFilled)
}

func paperScalpingRoundTripFees(entryPrice, exitPrice, baseFilled decimal.Decimal) decimal.Decimal {
	if entryPrice.LessThanOrEqual(decimal.Zero) || exitPrice.LessThanOrEqual(decimal.Zero) || baseFilled.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	rate := defaultPaperScalpingTakerFeeRate
	if configured, ok := paperScalpingTakerFeeRateFromEnv(); ok {
		rate = configured
	}
	entryNotional := entryPrice.Mul(baseFilled).Abs()
	exitNotional := exitPrice.Mul(baseFilled).Abs()
	return entryNotional.Add(exitNotional).Mul(rate)
}

func paperScalpingClosePositionLimit() int {
	if configured := getEnvInt("NEURATRADE_PAPER_SCALPING_CLOSE_POSITION_LIMIT"); configured > 0 {
		return configured
	}
	return defaultPaperScalpingCloseLimit
}

func paperScalpingTakerFeeRateFromEnv() (decimal.Decimal, bool) {
	raw := strings.TrimSpace(os.Getenv("NEURATRADE_PAPER_SCALPING_TAKER_FEE_RATE"))
	if raw == "" {
		return decimal.Zero, false
	}
	configured, err := decimal.NewFromString(raw)
	if err != nil || configured.IsNegative() {
		zaplogrus.Warnf("[SCALPING] Invalid paper scalping taker fee rate %q", raw)
		return decimal.Zero, false
	}
	return configured, true
}

func paperScalpingOutcomeFromNetPnL(netPnL decimal.Decimal) string {
	if netPnL.GreaterThan(decimal.Zero) {
		return "win"
	}
	if netPnL.LessThan(decimal.Zero) {
		return "loss"
	}
	return "breakeven"
}

func (h *IntegratedQuestHandlers) updatePaperScalpingTelemetryOutcome(
	ctx context.Context,
	orderID string,
	outcome string,
	netPnL decimal.Decimal,
	openedAt time.Time,
	closedAt time.Time,
) {
	if h == nil || h.telemetryStore == nil {
		return
	}
	holdSeconds := 0
	if !openedAt.IsZero() {
		holdSeconds = int(closedAt.Sub(openedAt).Seconds())
		if holdSeconds < 0 {
			holdSeconds = 0
		}
	}
	writeCtx, writeCancel := telemetryWriteContext()
	err := h.telemetryStore.UpdateCycleOutcome(writeCtx, orderID, ScalpingOutcomeRecord{
		Outcome:             outcome,
		PnL:                 netPnL.String(),
		HoldDurationSeconds: holdSeconds,
		ClosedAt:            closedAt,
	})
	writeCancel()
	if err != nil {
		zaplogrus.Warnf("[TELEMETRY] Failed to update paper outcome for order %s: %v", orderID, err)
	}
}
