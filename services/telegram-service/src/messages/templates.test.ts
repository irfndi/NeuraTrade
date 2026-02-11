import { describe, expect, test } from "bun:test";
import {
  formatActionAlertMessage,
  formatDoctorDiagnosticsMessage,
  formatMilestoneAlertMessage,
  formatPerformanceSummaryMessage,
  formatQuestProgressMessage,
  formatRiskEventMessage,
} from "./templates";

describe("message templates", () => {
  test("formats action alert in deterministic order", () => {
    const output = formatActionAlertMessage({
      action: "BUY",
      asset: "BTC/USDT",
      price: "42100",
      size: "0.25",
      strategy: "Scalping",
      reasoning: "Momentum breakout confirmed",
      riskCheckPassed: true,
      questId: "quest-17",
      timestamp: "2026-02-12T03:00:00Z",
      exchange: "Binance",
    });

    expect(output).toBe(
      "🤖 ACTION: BUY\n" +
        "Time: 2026-02-12T03:00:00Z\n" +
        "Asset: BTC/USDT\n" +
        "Exchange: Binance\n" +
        "Price: 42100\n" +
        "Size: 0.25\n" +
        "Strategy: Scalping\n" +
        "\n" +
        "Reasoning: Momentum breakout confirmed\n" +
        "\n" +
        "Risk Check: ✅ PASSED\n" +
        "Quest: quest-17",
    );
  });

  test("formats quest progress and computes percent when omitted", () => {
    const output = formatQuestProgressMessage({
      questId: "q-1",
      questName: "Volume Sprint",
      current: 3,
      target: 8,
      timeRemaining: "2h 10m",
    });

    expect(output).toBe(
      "📋 Quest: Volume Sprint\n" +
        "ID: q-1\n" +
        "Progress: 3/8 (38%)\n" +
        "Time Remaining: 2h 10m",
    );
  });

  test("omits optional milestone sections when empty", () => {
    const output = formatMilestoneAlertMessage({
      amount: "500",
      phase: "Growth",
    });

    expect(output).toBe("🎯 Milestone Reached: $500\nPhase: Growth");
  });

  test("formats risk event with sorted detail keys", () => {
    const output = formatRiskEventMessage({
      eventType: "kill_switch",
      severity: "critical",
      message: "All positions were closed automatically.",
      actionRequired: "Review exposure settings before resume.",
      timestamp: "2026-02-12T03:05:00Z",
      details: {
        zeta: "18%",
        alpha: "triggered",
      },
    });

    expect(output).toBe(
      "🚨 CRITICAL: kill_switch\n" +
        "\n" +
        "All positions were closed automatically.\n" +
        "\n" +
        "Action Required: Review exposure settings before resume.\n" +
        "Time: 2026-02-12T03:05:00Z\n" +
        "\n" +
        "Details:\n" +
        "- alpha: triggered\n" +
        "- zeta: 18%",
    );
  });

  test("formats performance summary and omits unset fields", () => {
    const output = formatPerformanceSummaryMessage({
      timeframe: "24h",
      pnl: "+$127.80",
      sharpe: "1.42",
      trades: 16,
    });

    expect(output).toBe(
      "📊 Performance Summary (24h)\n" +
        "PnL: +$127.80\n" +
        "Sharpe: 1.42\n" +
        "Trades: 16",
    );
  });

  test("formats doctor diagnostics with sorted checks and details", () => {
    const output = formatDoctorDiagnosticsMessage({
      overallStatus: "warning",
      summary: "One subsystem degraded",
      checkedAt: "2026-02-12T03:10:00Z",
      checks: [
        {
          name: "Redis",
          status: "warning",
          latencyMs: 78,
          message: "Latency elevated",
          details: {
            pool: "at-capacity",
            region: "sg-1",
          },
        },
        {
          name: "Backend API",
          status: "healthy",
          latencyMs: 22,
        },
      ],
    });

    expect(output).toBe(
      "⚠️ Doctor: WARNING\n" +
        "Checked At: 2026-02-12T03:10:00Z\n" +
        "Summary: One subsystem degraded\n" +
        "\n" +
        "Checks:\n" +
        "✅ Backend API: HEALTHY\n" +
        "   Latency: 22ms\n" +
        "⚠️ Redis: WARNING\n" +
        "   Latency elevated\n" +
        "   Latency: 78ms\n" +
        "   - pool: at-capacity\n" +
        "   - region: sg-1",
    );
  });
});
