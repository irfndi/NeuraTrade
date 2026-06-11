package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type ScalpingPaperSoakPersistenceOptions struct {
	ChatID      string
	Exchange    string
	Baseline    *ScalpingSoakBaseline
	OrderPrefix string
}

// PersistScalpingPaperBacktestSoakReport writes a no-order paper backtest into
// scalping telemetry and lifecycle tables, then returns the normal soak report.
func PersistScalpingPaperBacktestSoakReport(
	ctx context.Context,
	db DBPool,
	result *ScalpingBacktestResult,
	options ScalpingPaperSoakPersistenceOptions,
) (ScalpingSoakReport, error) {
	if isNilDBPool(db) {
		return ScalpingSoakReport{}, fmt.Errorf("persist scalping paper soak requires database")
	}
	if result == nil {
		return ScalpingSoakReport{}, fmt.Errorf("persist scalping paper soak requires backtest result")
	}
	if len(result.Signals) == 0 {
		return ScalpingSoakReport{}, fmt.Errorf("persist scalping paper soak requires signals")
	}

	telemetryStore := NewScalpingTelemetryStore(db, nil)
	if err := telemetryStore.EnsureSchema(ctx); err != nil {
		return ScalpingSoakReport{}, fmt.Errorf("ensure scalping telemetry schema: %w", err)
	}
	lifecycleStore, err := NewTradingLifecycleStore(db, nil)
	if err != nil {
		return ScalpingSoakReport{}, fmt.Errorf("create trading lifecycle store: %w", err)
	}
	paperRecorder := NewPaperTradeRecorder(db, nil)
	lifecycleStore.SetPaperRecorder(paperRecorder)

	chatID := strings.TrimSpace(options.ChatID)
	if chatID == "" {
		chatID = "paper-soak"
	}
	exchange := strings.TrimSpace(options.Exchange)
	if exchange == "" {
		exchange = strings.TrimSpace(result.Config.Exchange)
	}
	if exchange == "" {
		exchange = "paper"
	}
	orderPrefix := strings.TrimSpace(options.OrderPrefix)
	if orderPrefix == "" {
		orderPrefix = "paper-soak"
	}
	runPrefix := orderPrefix + "-" + uuid.NewString()

	tradesBySignal := map[string][]ScalpingBacktestTrade{}
	for _, trade := range result.Trades {
		key := paperSoakTradeKey(trade.EntryTime, trade.Symbol)
		tradesBySignal[key] = append(tradesBySignal[key], trade)
	}

	for i, signal := range result.Signals {
		orderID := ""
		action := "hold"
		trade, hasTrade := popPaperSoakTrade(tradesBySignal, paperSoakTradeKey(signal.Timestamp, signal.Symbol))
		if hasTrade {
			action = strings.ToLower(strings.TrimSpace(trade.Side))
			orderID = fmt.Sprintf("%s-%03d", runPrefix, i+1)
		}

		rejectionCounts, err := paperSoakRejectionCountsJSON(signal.RejectionReason)
		if err != nil {
			return ScalpingSoakReport{}, fmt.Errorf("encode paper soak rejection counts: %w", err)
		}
		cycleID, err := telemetryStore.InsertCycleRecord(ctx, CycleRecord{
			ID:                     fmt.Sprintf("%s-cycle-%03d", runPrefix, i+1),
			ChatID:                 chatID,
			Exchange:               exchange,
			OrderID:                orderID,
			CycleAt:                signal.Timestamp,
			Symbol:                 signal.Symbol,
			Action:                 action,
			Confidence:             paperSoakSignalConfidence(signal),
			UniverseCount:          result.Summary.TotalSignals,
			RankedCount:            result.Summary.TotalSignals,
			ViableCount:            result.Summary.EligibleSignals,
			RejectionCountsJSON:    rejectionCounts,
			Regime:                 signal.Regime,
			GateBlockCode:          strings.TrimSpace(signal.RejectionReason),
			EffectiveMinConfidence: result.Config.MinConfidence,
			EffectiveMaxCapitalPct: result.Config.MaxCapitalPct,
			SignalPrice:            finiteFloatPointer(signal.Signal.Price),
			BidAskSpreadPct:        finiteFloatPointer(signal.Signal.BidAskSpread),
			OrderBookImbalance:     finiteFloatPointer(signal.Signal.OrderBookImbalance),
			RangePosition24h:       finiteFloatPointer(signal.Signal.RangePosition24h),
			PriceChange24hPct:      finiteFloatPointer(signal.Signal.PriceChange24h),
			RecentPriceChangePct: finiteFloatPointerIf(
				signal.Signal.RecentPriceChange,
				signal.Signal.RecentChangeKnown,
			),
			RecentChangeAgeSec: finiteFloatPointerIf(
				signal.Signal.RecentChangeAgeSec,
				signal.Signal.RecentChangeKnown,
			),
		})
		if err != nil {
			return ScalpingSoakReport{}, fmt.Errorf("insert paper soak cycle: %w", err)
		}

		if !hasTrade {
			continue
		}
		if err := telemetryStore.LinkOrderToCycle(ctx, cycleID, orderID); err != nil {
			return ScalpingSoakReport{}, fmt.Errorf("link paper soak order to cycle: %w", err)
		}
		grossPnL := paperSoakGrossPnL(trade)
		if err := lifecycleStore.RecordClosedOrder(ctx, LifecycleCloseRecord{
			OrderID:     orderID,
			ChatID:      chatID,
			Exchange:    exchange,
			Symbol:      trade.Symbol,
			Side:        trade.Side,
			MarketType:  "futures",
			Filled:      trade.Size,
			EntryPrice:  trade.EntryPrice,
			ExitPrice:   trade.ExitPrice,
			RealizedPnL: grossPnL,
			Fees:        trade.Fees,
			Source:      scalpingPaperLifecycleSource,
			ClosedAt:    trade.ExitTime,
		}); err != nil {
			return ScalpingSoakReport{}, fmt.Errorf("record paper soak closed order: %w", err)
		}
		if err := telemetryStore.UpdateCycleOutcome(ctx, orderID, ScalpingOutcomeRecord{
			Outcome:             outcomeFromPnL(trade.PnL),
			PnL:                 trade.PnL.String(),
			HoldDurationSeconds: int(trade.ExitTime.Sub(trade.EntryTime).Seconds()),
			ClosedAt:            trade.ExitTime,
		}); err != nil {
			return ScalpingSoakReport{}, fmt.Errorf("update paper soak cycle outcome: %w", err)
		}
	}

	report, err := BuildScalpingSoakReport(ctx, db, ScalpingSoakReportFilter{
		ChatID:   chatID,
		Exchange: exchange,
		Since:    result.StartTime.Add(-result.Config.DefaultHoldPeriod),
		Until:    result.EndTime.Add(result.Config.DefaultHoldPeriod * 2),
		Baseline: options.Baseline,
	})
	if err != nil {
		return ScalpingSoakReport{}, fmt.Errorf("build persisted paper soak report: %w", err)
	}
	return report, nil
}

func paperSoakTradeKey(timestamp time.Time, symbol string) string {
	return timestamp.UTC().Format(time.RFC3339Nano) + "|" + normalizeSymbolForComparison(symbol)
}

func popPaperSoakTrade(tradesBySignal map[string][]ScalpingBacktestTrade, key string) (ScalpingBacktestTrade, bool) {
	trades := tradesBySignal[key]
	if len(trades) == 0 {
		return ScalpingBacktestTrade{}, false
	}
	trade := trades[0]
	if len(trades) == 1 {
		delete(tradesBySignal, key)
	} else {
		tradesBySignal[key] = trades[1:]
	}
	return trade, true
}

func paperSoakSignalConfidence(signal ScalpingBacktestSignal) float64 {
	if signal.Decision != nil {
		return signal.Decision.Confidence
	}
	return 0
}

func paperSoakRejectionCountsJSON(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "{}", nil
	}
	payload, err := json.Marshal(map[string]int{reason: 1})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func paperSoakGrossPnL(trade ScalpingBacktestTrade) decimal.Decimal {
	switch strings.ToLower(strings.TrimSpace(trade.Side)) {
	case "buy":
		return trade.ExitPrice.Sub(trade.EntryPrice).Mul(trade.Size)
	case "sell":
		return trade.EntryPrice.Sub(trade.ExitPrice).Mul(trade.Size)
	default:
		return trade.PnL.Add(trade.Fees)
	}
}
