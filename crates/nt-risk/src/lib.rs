//! Pre-trade risk guards — the only path to execution.
//! Mirrors `services/neuratrade-cli-ts/src/risk/guards.ts` semantics.
//!
//! Money is integer micro-USDT (`Money`), never float — including formatting.
//! Percentages are computed with `i128` intermediates, truncated toward zero.

/// Micro-USDT: 1 USDT = 1_000_000 units.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct Money(pub i64);

impl Money {
    pub const ZERO: Money = Money(0);

    /// Whole USDT (truncates sub-USDT dust).
    pub fn usdt(v: i64) -> Money {
        Money(v.saturating_mul(1_000_000))
    }

    /// Integer `D.cc` rendering, e.g. 49490000 -> "49.49", -5000 -> "-0.00".
    pub fn render(self) -> String {
        let neg = self.0 < 0;
        let a = self.0.unsigned_abs();
        format!(
            "{}{}.{:02}",
            if neg { "-" } else { "" },
            a / 1_000_000,
            (a % 1_000_000) / 10_000
        )
    }
}

/// Risk limits. `*_pct` fields are whole percent (e.g. 100 = 100%).
/// Not `Copy` (holds `Vec<String>` allowlists) — pass by reference, or
/// `.clone()` where an owned copy is actually needed.
#[derive(Debug, Clone)]
pub struct RiskLimits {
    pub min_capital: Money,
    pub max_position_size_pct: i64,
    pub max_notional_pct: i64,
    pub max_drawdown_pct: i64,
    pub max_daily_loss_pct: i64,
    /// Live-trading kill switch. Mirrors guards.ts `liveTradingEnabled`.
    pub live_trading_enabled: bool,
    /// Daily fill cap. Mirrors guards.ts `maxTradesPerDay`.
    pub max_trades_per_day: u32,
    /// Symbol allowlist; `None` or empty means unrestricted.
    pub allowed_symbols: Option<Vec<String>>,
    /// Product-type allowlist; `None` or empty means unrestricted.
    pub allowed_product_types: Option<Vec<String>>,
    /// Leverage cap; `None` means unrestricted.
    pub max_leverage: Option<i64>,
}

impl RiskLimits {
    /// Live defaults mirror `defaultRiskLimits` (live) in guards.ts.
    ///
    /// `min_capital` is 100 here, but production runs 10 USDT per symbol and
    /// `nt-cli shadow` lowers the floor to 1 for that reason. Note this gate
    /// checks the STARTING capital only (`basic_risk_violations`), so it is a
    /// launch sanity check — equity protection is the drawdown / daily-loss
    /// gates' job. Any caller using `RiskLimits::live()` directly against a
    /// per-symbol partition rejects EVERY entry — pin the floor explicitly or
    /// a parity diff is a config artifact, not divergence.
    pub fn live() -> RiskLimits {
        RiskLimits {
            min_capital: Money::usdt(100),
            max_position_size_pct: 10,
            max_notional_pct: 100,
            max_drawdown_pct: 15,
            max_daily_loss_pct: 5,
            live_trading_enabled: true,
            max_trades_per_day: 10,
            allowed_symbols: None,
            allowed_product_types: Some(vec!["USDT-FUTURES".to_string()]),
            max_leverage: Some(10),
        }
    }
}

/// Equity window for drawdown / daily-loss guards.
#[derive(Debug, Clone, Copy)]
pub struct EquityWindow {
    pub current: Money,
    pub peak: Money,
    pub day_start: Money,
}

/// `basicRiskViolations`: capital floor. Fail-closed on any violation.
pub fn basic_risk_violations(capital: Money, limits: &RiskLimits) -> Vec<String> {
    let mut out = Vec::new();
    if capital < limits.min_capital {
        out.push(format!(
            "capital {} is below minimum {}",
            capital.render(),
            limits.min_capital.render()
        ));
    }
    out
}

/// Live-trading kill switch. Mirrors guards.ts `liveTradingEnabled` check
/// (folded into `basicRiskViolations` there; kept as its own function here
/// so it composes with [`approve`] additively — see note above `approve`).
pub fn live_trading_violations(is_live: bool, limits: &RiskLimits) -> Vec<String> {
    let mut out = Vec::new();
    if is_live && !limits.live_trading_enabled {
        out.push("live trading is disabled".to_string());
    }
    out
}

/// Daily fill cap. Mirrors guards.ts `maxTradesPerDay` check.
pub fn trade_count_violations(trades_today_count: u32, limits: &RiskLimits) -> Vec<String> {
    let mut out = Vec::new();
    if trades_today_count >= limits.max_trades_per_day {
        out.push(format!(
            "trades today {trades_today_count} meets or exceeds max {}",
            limits.max_trades_per_day
        ));
    }
    out
}

/// `allowlistRiskViolations`: symbol/product-type allowlists + leverage cap.
/// `product_type: None` is treated the same as guards.ts's `undefined` —
/// it fails an active product-type allowlist rather than passing it.
pub fn allowlist_violations(
    symbol: &str,
    product_type: Option<&str>,
    leverage: Option<i64>,
    limits: &RiskLimits,
) -> Vec<String> {
    let mut out = Vec::new();
    if let Some(allowed) = &limits.allowed_symbols
        && !allowed.is_empty()
        && !allowed.iter().any(|s| s == symbol)
    {
        out.push(format!("symbol {symbol} is not in the allowed list"));
    }
    if let Some(allowed) = &limits.allowed_product_types
        && !allowed.is_empty()
    {
        match product_type {
            None => out.push("product type unknown is not allowed".to_string()),
            Some(pt) if !allowed.iter().any(|s| s == pt) => {
                out.push(format!("product type {pt} is not allowed"));
            }
            _ => {}
        }
    }
    if let (Some(max_lev), Some(lev)) = (limits.max_leverage, leverage)
        && lev > max_lev
    {
        out.push(format!("leverage {lev}x exceeds max {max_lev}x"));
    }
    out
}

/// Micro-USDT percent of `part` over `whole`: `part*100/whole`, zero when broke.
fn pct_of(part: Money, whole: Money) -> i64 {
    if whole.0 <= 0 {
        return 0;
    }
    ((part.0 as i128 * 100 / whole.0 as i128) as i64).max(0)
}

/// Drawdown + daily-loss guards: `(peak-current)*100/peak` and
/// `(day_start-current)*100/day_start`. Profits never violate.
pub fn drawdown_violations(w: &EquityWindow, limits: &RiskLimits) -> Vec<String> {
    let mut out = Vec::new();
    if w.peak.0 > 0 {
        let dd = ((w.peak.0 - w.current.0).max(0) as i128 * 100 / w.peak.0 as i128) as i64;
        if dd > limits.max_drawdown_pct {
            out.push(format!(
                "drawdown {dd}% exceeds max {}%",
                limits.max_drawdown_pct
            ));
        }
    }
    if w.day_start.0 > 0 {
        let dl =
            ((w.day_start.0 - w.current.0).max(0) as i128 * 100 / w.day_start.0 as i128) as i64;
        if dl > limits.max_daily_loss_pct {
            out.push(format!(
                "daily loss {dl}% exceeds max {}%",
                limits.max_daily_loss_pct
            ));
        }
    }
    out
}

/// `positionRiskViolations`: size / notional / orderability caps.
pub fn position_risk_violations(
    position_value: Money,
    notional_value: Money,
    min_orderable: Money,
    leverage: i64,
    capital: Money,
    limits: &RiskLimits,
) -> Vec<String> {
    let mut out = Vec::new();
    let lev = leverage.max(1);
    let size_pct = pct_of(Money(position_value.0 / lev), capital);
    if size_pct > limits.max_position_size_pct {
        out.push(format!(
            "position size {size_pct}% exceeds max {}%",
            limits.max_position_size_pct
        ));
    }
    let notional_pct = pct_of(notional_value, capital);
    if notional_pct > limits.max_notional_pct {
        out.push(format!(
            "notional {notional_pct}% exceeds max {}%",
            limits.max_notional_pct
        ));
    }
    let min_pct = pct_of(Money(min_orderable.0 / lev), capital);
    if min_pct > limits.max_position_size_pct {
        out.push(format!(
            "minimum orderable position {min_pct}% exceeds max {}%",
            limits.max_position_size_pct
        ));
    }
    out
}

/// Seal: only [`approve`]/[`approve_full`] can mint one.
/// `nt-execution::submit` demands it, so unapproved orders cannot reach
/// execution — in Rust or any FFI.
#[derive(Debug, Clone, Copy)]
pub struct RiskApproval {
    _seal: (),
}
/// Throughput notes on the `Ok` path: non-empty means halt new entries.
pub type ThroughputNotes = Vec<String>;
/// Approved book + throughput notes.
pub type GateOk = (RiskApproval, ThroughputNotes);
/// Hard violations + throughput violations.
pub type GateErr = (Vec<String>, Vec<String>);
/// Bundled gate decision.
pub type GateDecision = Result<GateOk, GateErr>;

/// Full pre-trade gate: floor + drawdown + position caps (+ live/count/
/// allowlist via [`approve_full`] with default paper intent).
/// `Ok` carries the execution seal; `Err` carries every violation.
///
/// Legacy wrapper for callers without live/daily/throughput context: passes
/// a default paper intent and an empty throughput window.
#[derive(Debug, Clone)]
pub struct TradeIntent {
    pub is_live: bool,
    pub trades_today: u32,
    pub symbol: String,
    pub product_type: Option<String>,
    pub leverage: Option<i64>,
}
impl Default for TradeIntent {
    fn default() -> TradeIntent {
        TradeIntent {
            is_live: false,
            trades_today: 0,
            symbol: "PAPER".to_string(),
            product_type: Some("USDT-FUTURES".to_string()),
            leverage: None,
        }
    }
}
/// Per-symbol rolling throughput tracker keyed by `exchange:symbol:timeframe`.
/// Stores `(ts_ms, gross_micros, fee_micros)` fills; [`ThroughputTracker::window`]
/// evicts entries older than `window_ms` and aggregates the rest.
#[derive(Debug, Default)]
pub struct ThroughputTracker {
    fills: std::collections::HashMap<String, Vec<(i64, i64, i64)>>,
}
impl ThroughputTracker {
    pub fn key(exchange: &str, symbol: &str, timeframe: &str) -> String {
        format!("{exchange}:{symbol}:{timeframe}")
    }
    pub fn record(&mut self, key: &str, ts_ms: i64, gross_micros: i64, fee_micros: i64) {
        self.fills
            .entry(key.to_string())
            .or_default()
            .push((ts_ms, gross_micros, fee_micros));
    }
    pub fn window(&mut self, key: &str, now_ms: i64, window_ms: i64) -> ThroughputWindow {
        let v = self.fills.entry(key.to_string()).or_default();
        v.retain(|(ts, _, _)| now_ms - *ts < window_ms);
        let mut w = ThroughputWindow::default();
        for (_, g, f) in v.iter() {
            w.trades += 1;
            w.gross_micros = w.gross_micros.saturating_add(*g);
            w.fees_micros = w.fees_micros.saturating_add(*f);
        }
        w
    }
}
pub fn approve(
    capital: Money,
    window: &EquityWindow,
    position_value: Money,
    notional_value: Money,
    min_orderable: Money,
    leverage: i64,
    limits: &RiskLimits,
) -> Result<RiskApproval, Vec<String>> {
    approve_full(
        capital,
        window,
        position_value,
        notional_value,
        min_orderable,
        leverage,
        limits,
        &TradeIntent::default(),
        &ThroughputWindow::default(),
        &ThroughputLimits::strict(),
    )
    .map(|(a, _)| a)
    .map_err(|(v, _)| v)
}
/// Bundled gate (clever-cabin-k4u, one pass): floor + drawdown + position +
/// live/count/allowlist + throughput. Throughput returns on BOTH paths:
/// `Ok((seal, t))` with non-empty `t` means approved book but halt the symbol
/// (no new entries); exits ignore `t` so a halt never strands inventory.
#[allow(clippy::too_many_arguments)] // ponytail: bundled k4u gate — one 10-arg call beats two passes over the same call sites
pub fn approve_full(
    capital: Money,
    window: &EquityWindow,
    position_value: Money,
    notional_value: Money,
    min_orderable: Money,
    leverage: i64,
    limits: &RiskLimits,
    intent: &TradeIntent,
    throughput: &ThroughputWindow,
    tp_limits: &ThroughputLimits,
) -> GateDecision {
    let mut v = basic_risk_violations(capital, limits);
    v.extend(drawdown_violations(window, limits));
    v.extend(position_risk_violations(
        position_value,
        notional_value,
        min_orderable,
        leverage,
        capital,
        limits,
    ));
    v.extend(live_trading_violations(intent.is_live, limits));
    v.extend(trade_count_violations(intent.trades_today, limits));
    v.extend(allowlist_violations(
        &intent.symbol,
        intent.product_type.as_deref(),
        intent.leverage.or(Some(leverage)),
        limits,
    ));
    let t = throughput_violations(throughput, tp_limits);
    if v.is_empty() {
        Ok((RiskApproval { _seal: () }, t))
    } else {
        Err((v, t))
    }
}

/// Hourly throughput window for the revenge-spiral breaker (P2).
/// `gross_micros` is signed window PnL; `fees_micros` is total fees paid.
#[derive(Debug, Clone, Copy, Default)]
pub struct ThroughputWindow {
    pub trades: u64,
    pub gross_micros: i64,
    pub fees_micros: i64,
}

/// Throughput breaker limits.
#[derive(Debug, Clone, Copy)]
pub struct ThroughputLimits {
    /// Halt a symbol past this many fills in the window (e.g. 25/hour).
    pub max_trades_per_window: u64,
    /// Halt when fees exceed this share (bp) of activity `|gross|+fees`.
    pub max_fee_share_bp: i64,
}

impl ThroughputLimits {
    /// P2 proposal: 25 trades/hour or 50% fee share halts the symbol.
    pub fn strict() -> ThroughputLimits {
        ThroughputLimits {
            max_trades_per_window: 25,
            max_fee_share_bp: 5_000,
        }
    }
}

/// Revenge-spiral breaker: count cap first, then fee-drag share.
/// Fee share = `fees*10000/(|gross|+fees)`; pure-fee windows read 100%.
pub fn throughput_violations(w: &ThroughputWindow, lim: &ThroughputLimits) -> Vec<String> {
    let mut out = Vec::new();
    if w.trades > lim.max_trades_per_window {
        out.push(format!(
            "throughput {} trades exceeds max {} per window",
            w.trades, lim.max_trades_per_window
        ));
    }
    let activity = (w.gross_micros as i128).abs() + w.fees_micros as i128;
    if activity > 0 {
        let share = (w.fees_micros as i128 * 10_000 / activity) as i64;
        if share > lim.max_fee_share_bp {
            out.push(format!(
                "fee share {share}bp exceeds max {}bp",
                lim.max_fee_share_bp
            ));
        }
    }
    out
}
