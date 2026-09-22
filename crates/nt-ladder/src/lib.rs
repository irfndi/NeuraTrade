//! Multi-rung ladder paper state (P5 Step 3, slice 1).
//!
//! Deliberately separate from `nt_grid`: `sleeves.rs` documents the grid
//! engine as "frozen at single-position lev-1 parity", and the ladder is a
//! different engine (multi-rung, two independent side ladders, persisted
//! per-rung step). Importing `nt_grid::Side` here would couple two engines
//! that share nothing but a name.
//!
//! Slice 1 is STATE ONLY — no fills, no seeding, no parity. The TS ground
//! truth is `LadderPaperState` / `LadderPaperRungState`
//! (`services/neuratrade-cli-ts/src/paper-trading/types.ts:246-306`) and
//! `WorkingState` (`ladder-engine.ts:255-265`).
//!
//! Money is `nt_risk::Money` (i64 micro-USDT) everywhere. TS stores levels,
//! steps and bases as float dollars, so a 1:1 micros conversion needs the
//! rounding policy that slice 3 settles — until then, state round-trip
//! fidelity is what this module proves, not numeric parity with TS.
//!
//! `entry_bar` is stored ABSOLUTE (bars consumed since the state's first
//! bar), not window-relative as TS does (`types.ts:259-261`). TS gets away
//! with window-relative because it resolves the index into the freshly
//! fetched array each tick; a resume that carries a window-relative index
//! across ticks would compare it against a different window. Recorded as a
//! deviation from TS.

use nt_grid::account_scaled_leverage_cap;
use nt_risk::Money;

/// Which ladder a rung belongs to. TS: `LadderSide = "long" | "short"`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Side {
    Long,
    Short,
}

impl Side {
    pub fn as_str(self) -> &'static str {
        match self {
            Side::Long => "long",
            Side::Short => "short",
        }
    }

    pub fn parse(s: &str) -> Option<Self> {
        match s {
            "long" => Some(Side::Long),
            "short" => Some(Side::Short),
            _ => None,
        }
    }
}

/// One rung of a side ladder.
///
/// `filled_qty` is base units in micros; `0` means unfilled. TS uses an
/// optional `filledQty` (`types.ts:265`), but 0 is unambiguous for a base
/// quantity and keeps the resume format line-shaped.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Rung {
    /// 1-based rung index within the side ladder.
    pub rung_index: u32,
    pub side: Side,
    /// Entry level (pre-slippage), micro-USDT.
    pub level: Money,
    /// Grid step in force at SEED time, micro-USDT. Persisted per rung —
    /// TS does the same (`types.ts:255-256`) rather than re-deriving from
    /// the base, because the base can re-anchor while a rung is armed.
    pub step: Money,
    pub filled: bool,
    /// Post-slippage entry price, micro-USDT. 0 while unfilled.
    pub entry_price: Money,
    /// Absolute bar index of the fill (see module note on window-relative).
    pub entry_bar: u64,
    /// Absolute fill time, ms epoch — the live max-hold clock.
    pub entry_ts_ms: i64,
    /// Filled quantity in base micros; 0 while unfilled.
    pub filled_qty: i64,
}

impl Rung {
    /// An unfilled rung at `level`, spaced `step` from its neighbours.
    pub fn armed(rung_index: u32, side: Side, level: Money, step: Money) -> Self {
        Rung {
            rung_index,
            side,
            level,
            step,
            filled: false,
            entry_price: Money::ZERO,
            entry_bar: 0,
            entry_ts_ms: 0,
            filled_qty: 0,
        }
    }

    pub fn is_filled(&self) -> bool {
        self.filled
    }

    /// Rungs are filled iff `filled` AND a positive quantity was booked —
    /// TS's optional `filledQty` makes a bare `filled` flag insufficient.
    pub fn is_open(&self) -> bool {
        self.filled && self.filled_qty > 0
    }
}

/// Config fingerprint for mismatch detection (slice 1 stores it; the
/// reseed-on-mismatch policy is slice 2).
///
/// Units follow the TS option types (`LadderPaperTradingOptions`): percents
/// and ratios are stored as whole basis-point-free integers by convention
/// here — `grid_step_bp` is bp, ratios are `x100`, counts are raw. Slice 2
/// defines the comparison against live options.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct LadderConfig {
    pub grid_step_bp: i64,
    pub grid_max_grids: i64,
    pub grid_pause_after_loss_bars: i64,
    pub rungs: u32,
    pub target_ratio_x100: i64,
    pub only_with_trend: bool,
    pub chop_gate_adx: i64,
    pub max_hold_bars: i64,
    /// Stop distance as a multiple of the grid step; 0 = legacy boundary.
    pub stop_ratio_x100: i64,
    pub conservative_intrabar: bool,
    // NOTE: sizing knobs (maxPositionPct / maxNotionalPct / leverage /
    // fullyDynamicLeverage / maxLeverage) deliberately do NOT live here. TS
    // persists exactly these 10 fingerprint fields and compares exactly them
    // (`matchesLadderGridSettings`), while `configMatchesLadderState` ignores
    // sizing entirely. Adding them would silently drop them on the
    // save/load round trip and force-reseed on every tick. They belong on
    // `SizingOptions`, which is never persisted.
}

/// Persistent ladder state: 1:1 with TS `LadderPaperState`
/// (`types.ts:275-306`) minus the `exchange`/`symbol`/`timeframe` identity
/// triple, which the caller keys on rather than the state carrying.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LadderState {
    /// Config-level starting capital the state was seeded under.
    pub initial_capital: Money,
    pub capital: Money,
    pub peak_capital: Money,
    pub total_wins: u64,
    pub total_losses: u64,
    pub long_rungs: Vec<Rung>,
    pub short_rungs: Vec<Rung>,
    /// Per-side anchor, micro-USDT. 0 = that side has never been seeded.
    pub long_base: Money,
    pub short_base: Money,
    pub paused: u64,
    pub config: LadderConfig,
    /// Timestamp of the last processed candle (the tick cursor).
    pub last_ts_ms: Option<i64>,
    /// Bars consumed since the state's first bar — the absolute counter
    /// `Rung::entry_bar` is measured against.
    pub bars_consumed: u64,
}

impl LadderState {
    /// Fresh state under `config`, seeded with `initial_capital` and no
    /// rungs. Mirrors TS `freshLadderState` (`ladder-engine.ts:1007`) at
    /// the state level only — the seeding rules are slice 2.
    pub fn fresh(initial_capital: Money, config: LadderConfig) -> Self {
        LadderState {
            initial_capital,
            capital: initial_capital,
            peak_capital: initial_capital,
            total_wins: 0,
            total_losses: 0,
            long_rungs: Vec::new(),
            short_rungs: Vec::new(),
            long_base: Money::ZERO,
            short_base: Money::ZERO,
            paused: 0,
            config,
            last_ts_ms: None,
            bars_consumed: 0,
        }
    }

    pub fn rungs(&self, side: Side) -> &[Rung] {
        match side {
            Side::Long => &self.long_rungs,
            Side::Short => &self.short_rungs,
        }
    }

    pub fn rungs_mut(&mut self, side: Side) -> &mut Vec<Rung> {
        match side {
            Side::Long => &mut self.long_rungs,
            Side::Short => &mut self.short_rungs,
        }
    }

    pub fn base(&self, side: Side) -> Money {
        match side {
            Side::Long => self.long_base,
            Side::Short => self.short_base,
        }
    }

    /// Open (filled with positive qty) rung count across both sides.
    pub fn open_rung_count(&self) -> usize {
        self.long_rungs
            .iter()
            .chain(self.short_rungs.iter())
            .filter(|r| r.is_open())
            .count()
    }

    /// Flat = no open rungs and no armed rungs on either side. TS `isFlat`
    /// (`ladder-engine.ts:2094`) drives the force-reseed decision.
    pub fn is_flat(&self) -> bool {
        self.long_rungs.is_empty() && self.short_rungs.is_empty()
    }
}

// ---------------------------------------------------------------------------
// Resume (line-based, v3 — ladder-tagged so a grid resume fails loudly)
// ---------------------------------------------------------------------------

/// Current resume format version. `nt-cli`'s grid path is at `v2`
/// (`main.rs` writes a `v2` header plus a day triple). A ladder resume is a
/// different payload with variable-length rung vectors, so it gets its own
/// tag rather than a third field set on the grid format: feeding a grid file
/// to the ladder path must error, not silently default to zeros.
pub const RESUME_VERSION: &str = "ladder v3";

/// Resume parse failure. Deliberately distinct from any grid-path error so
/// a caller cannot mistake one for the other.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ResumeError {
    /// First line was not `ladder v3` — most often a grid resume file.
    BadVersion(String),
    /// A line had the right keyword but an unparseable value.
    BadField(&'static str, String),
    /// A required line was missing entirely.
    MissingField(&'static str),
    /// A rung line had the wrong column count.
    BadRungLine(usize),
}

impl std::fmt::Display for ResumeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ResumeError::BadVersion(v) => write!(
                f,
                "resume version {v:?} is not {RESUME_VERSION:?} — a grid resume file cannot be read as a ladder"
            ),
            ResumeError::BadField(k, v) => write!(f, "resume field {k} has bad value {v:?}"),
            ResumeError::MissingField(k) => write!(f, "resume is missing required field {k}"),
            ResumeError::BadRungLine(n) => write!(f, "resume rung line {n} has wrong column count"),
        }
    }
}

impl std::error::Error for ResumeError {}

fn num(s: &str) -> Result<i64, ResumeError> {
    s.parse::<i64>()
        .map_err(|_| ResumeError::BadField("number", s.to_string()))
}

fn push_line(out: &mut String, key: &str, value: impl std::fmt::Display) {
    out.push_str(key);
    out.push(' ');
    out.push_str(&value.to_string());
    out.push('\n');
}

/// Serialize a rung as one line: `rung <index> <side> <level> <step>
/// <filled 0|1> <entry_price> <entry_bar> <entry_ts> <filled_qty>`.
fn rung_line(r: &Rung) -> String {
    format!(
        "rung {} {} {} {} {} {} {} {} {}\n",
        r.rung_index,
        r.side.as_str(),
        r.level.0,
        r.step.0,
        u8::from(r.filled),
        r.entry_price.0,
        r.entry_bar,
        r.entry_ts_ms,
        r.filled_qty,
    )
}

/// Write `state` in the `ladder v3` resume format.
pub fn save(state: &LadderState) -> String {
    let mut out = String::from(RESUME_VERSION);
    out.push('\n');
    push_line(&mut out, "initial_capital", state.initial_capital.0);
    push_line(&mut out, "capital", state.capital.0);
    push_line(&mut out, "peak_capital", state.peak_capital.0);
    push_line(&mut out, "total_wins", state.total_wins);
    push_line(&mut out, "total_losses", state.total_losses);
    push_line(&mut out, "long_base", state.long_base.0);
    push_line(&mut out, "short_base", state.short_base.0);
    push_line(&mut out, "paused", state.paused);
    push_line(&mut out, "grid_step_bp", state.config.grid_step_bp);
    push_line(&mut out, "grid_max_grids", state.config.grid_max_grids);
    push_line(
        &mut out,
        "grid_pause_after_loss_bars",
        state.config.grid_pause_after_loss_bars,
    );
    push_line(&mut out, "rungs", state.config.rungs);
    push_line(
        &mut out,
        "target_ratio_x100",
        state.config.target_ratio_x100,
    );
    push_line(
        &mut out,
        "only_with_trend",
        u8::from(state.config.only_with_trend),
    );
    push_line(&mut out, "chop_gate_adx", state.config.chop_gate_adx);
    push_line(&mut out, "max_hold_bars", state.config.max_hold_bars);
    push_line(&mut out, "stop_ratio_x100", state.config.stop_ratio_x100);
    push_line(
        &mut out,
        "conservative_intrabar",
        u8::from(state.config.conservative_intrabar),
    );
    match state.last_ts_ms {
        Some(ts) => push_line(&mut out, "last_ts", ts),
        None => out.push_str("last_ts none\n"),
    }
    push_line(&mut out, "bars_consumed", state.bars_consumed);
    for r in state.long_rungs.iter().chain(state.short_rungs.iter()) {
        out.push_str(&rung_line(r));
    }
    out
}

/// Parse a `ladder v3` resume file. Any other version — including a grid
/// `v2` file — is a hard error.
pub fn load(text: &str) -> Result<LadderState, ResumeError> {
    let mut lines = text.lines();
    let header = lines.next().unwrap_or("").trim();
    if header != RESUME_VERSION {
        return Err(ResumeError::BadVersion(header.to_string()));
    }

    // Seed with an all-zero config so every scalar field is distinguishable
    // from "parsed 0". A truncated file must FAIL, not parse as a valid
    // zero-capital state: slice 2 compares this fingerprint against live
    // options to decide force-reseed, and an all-zero fingerprint would
    // make that decision meaningless.
    let mut st = LadderState::fresh(
        Money::ZERO,
        LadderConfig {
            grid_step_bp: 0,
            grid_max_grids: 0,
            grid_pause_after_loss_bars: 0,
            rungs: 0,
            target_ratio_x100: 0,
            only_with_trend: false,
            chop_gate_adx: 0,
            max_hold_bars: 0,
            stop_ratio_x100: 0,
            conservative_intrabar: true,
        },
    );

    // Required scalar fields. Absence is a truncated file, which is an error
    // rather than a silent default.
    let mut seen: Vec<&'static str> = Vec::new();
    let require = |key: &'static str, seen: &mut Vec<&'static str>| {
        if !seen.contains(&key) {
            seen.push(key);
        }
    };

    // `enumerate()` from 1 so BadRungLine reports the TRUE file line: the
    // header is line 1, so data lines start at 2.
    for (lineno, line) in lines.enumerate() {
        let lineno = lineno + 2; // header consumed separately
        let f: Vec<&str> = line.split_whitespace().collect();
        if f.is_empty() {
            continue;
        }
        match f.as_slice() {
            ["initial_capital", v] => {
                st.initial_capital = Money(num(v)?);
                require("initial_capital", &mut seen);
            }
            ["capital", v] => {
                st.capital = Money(num(v)?);
                require("capital", &mut seen);
            }
            ["peak_capital", v] => {
                st.peak_capital = Money(num(v)?);
                require("peak_capital", &mut seen);
            }
            ["total_wins", v] => {
                st.total_wins = num(v)? as u64;
                require("total_wins", &mut seen);
            }
            ["total_losses", v] => {
                st.total_losses = num(v)? as u64;
                require("total_losses", &mut seen);
            }
            ["long_base", v] => {
                st.long_base = Money(num(v)?);
                require("long_base", &mut seen);
            }
            ["short_base", v] => {
                st.short_base = Money(num(v)?);
                require("short_base", &mut seen);
            }
            ["paused", v] => {
                st.paused = num(v)? as u64;
                require("paused", &mut seen);
            }
            ["grid_step_bp", v] => {
                st.config.grid_step_bp = num(v)?;
                require("grid_step_bp", &mut seen);
            }
            ["grid_max_grids", v] => {
                st.config.grid_max_grids = num(v)?;
                require("grid_max_grids", &mut seen);
            }
            ["grid_pause_after_loss_bars", v] => {
                st.config.grid_pause_after_loss_bars = num(v)?;
                require("grid_pause_after_loss_bars", &mut seen);
            }
            ["rungs", v] => {
                st.config.rungs = num(v)? as u32;
                require("rungs", &mut seen);
            }
            ["target_ratio_x100", v] => {
                st.config.target_ratio_x100 = num(v)?;
                require("target_ratio_x100", &mut seen);
            }
            ["only_with_trend", v] => {
                st.config.only_with_trend = num(v)? != 0;
                require("only_with_trend", &mut seen);
            }
            ["chop_gate_adx", v] => {
                st.config.chop_gate_adx = num(v)?;
                require("chop_gate_adx", &mut seen);
            }
            ["max_hold_bars", v] => {
                st.config.max_hold_bars = num(v)?;
                require("max_hold_bars", &mut seen);
            }
            ["stop_ratio_x100", v] => {
                st.config.stop_ratio_x100 = num(v)?;
                require("stop_ratio_x100", &mut seen);
            }
            ["conservative_intrabar", v] => {
                st.config.conservative_intrabar = num(v)? != 0;
                require("conservative_intrabar", &mut seen);
            }
            ["last_ts", "none"] => {
                st.last_ts_ms = None;
                require("last_ts", &mut seen);
            }
            ["last_ts", v] => {
                st.last_ts_ms = Some(num(v)?);
                require("last_ts", &mut seen);
            }
            ["bars_consumed", v] => {
                st.bars_consumed = num(v)? as u64;
                require("bars_consumed", &mut seen);
            }
            ["rung", index, side, level, step, filled, ep, ebar, ets, qty] => {
                let side = Side::parse(side)
                    .ok_or_else(|| ResumeError::BadField("rung side", side.to_string()))?;
                let rung = Rung {
                    rung_index: num(index)? as u32,
                    side,
                    level: Money(num(level)?),
                    step: Money(num(step)?),
                    filled: num(filled)? != 0,
                    entry_price: Money(num(ep)?),
                    entry_bar: num(ebar)? as u64,
                    entry_ts_ms: num(ets)?,
                    filled_qty: num(qty)?,
                };
                match side {
                    Side::Long => st.long_rungs.push(rung),
                    Side::Short => st.short_rungs.push(rung),
                }
            }
            _ => return Err(ResumeError::BadRungLine(lineno)),
        }
    }

    // Every required field must be present exactly once-worth: absent means a
    // truncated write (the tick loop rewrites this file every interval, so a
    // partial file is a real failure mode, not a theoretical one).
    const REQUIRED: [&str; 20] = [
        "initial_capital",
        "capital",
        "peak_capital",
        "total_wins",
        "total_losses",
        "long_base",
        "short_base",
        "paused",
        "grid_step_bp",
        "grid_max_grids",
        "grid_pause_after_loss_bars",
        "rungs",
        "target_ratio_x100",
        "only_with_trend",
        "chop_gate_adx",
        "max_hold_bars",
        "stop_ratio_x100",
        "conservative_intrabar",
        "last_ts",
        "bars_consumed",
    ];
    for key in REQUIRED {
        if !seen.contains(&key) {
            return Err(ResumeError::MissingField(key));
        }
    }

    Ok(st)
}

// ---------------------------------------------------------------------------
// Slice 2: the a07b3dd0 seed rule
// ---------------------------------------------------------------------------

/// Per-bar context the seed decision needs. Deliberately minimal: the trend
/// gate is expressed as an `Option<Money>` (None = no filter configured),
/// which is how TS's `ctx.trend === null` reads.
#[derive(Debug, Clone, Copy)]
pub struct SeedContext {
    /// The bar's open — the anchor for a NEW seed only.
    pub open: Money,
    /// The bar's close — the trend gate compares against this.
    pub close: Money,
    /// Grid step in price units, derived by the caller from the bar's open.
    pub step: Money,
    /// Trend SMA value; None disables the trend gate.
    pub trend: Option<Money>,
    /// When true, longs need close > trend and shorts need close < trend.
    pub only_with_trend: bool,
    /// Chop gate: when active, no new seed is created.
    pub chop_gate_active: bool,
    /// Account drawdown breached: when true, no new seed is created.
    pub drawdown_breached: bool,
    /// Number of rungs per side (`opts.rungs`).
    pub rung_count: u32,
}

/// What a seed attempt did. Returned rather than applied so the caller
/// controls mutation — mirrors how TS's `seedLadderSide` is one of several
/// per-bar mutators and keeps this function pure and testable.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SeedOutcome {
    /// A side already has a filled rung; nothing may change.
    FilledRungsPresent,
    /// A side has armed (unfilled) rungs; they are KEPT untouched. This is
    /// the `a07b3dd0` invariant: a blocked bar must not wipe armed rungs.
    ArmedRungsKept,
    /// A gate blocked the seed and the side was empty, so it stays empty.
    BlockedEmpty,
    /// A new seed was created.
    Seeded,
}

fn side_rungs_mut(state: &mut LadderState, side: Side) -> &mut Vec<Rung> {
    state.rungs_mut(side)
}

/// Port of TS `seedLadderSide` (`ladder-engine.ts:603-627`).
///
/// Three-tier precedence, in this exact order:
/// 1. A filled rung exists → return, nothing may move.
/// 2. Armed rungs exist → return, they are KEPT. Gates apply to the EMPTY
///    seed only; a blocked bar must never wipe armed rungs.
/// 3. Otherwise evaluate gates; blocked → stay empty, allowed → seed.
///
/// The anti-pattern this replaces: re-deriving levels from the current bar's
/// open on every flat bar (as `nt_grid`'s single-position engine does) and
/// adding a gate check before it, which wipes armed rungs on any blocked
/// bar. Once seeded, the rungs' persisted `level`/`step` are the only
/// source of truth and no later bar may move them.
pub fn seed_side(state: &mut LadderState, side: Side, ctx: &SeedContext) -> SeedOutcome {
    // Tier 1: a filled rung exists.
    if state.rungs(side).iter().any(|r| r.filled) {
        return SeedOutcome::FilledRungsPresent;
    }
    // Tier 2: armed rungs exist — keep them, gates do not apply.
    if !state.rungs(side).is_empty() {
        return SeedOutcome::ArmedRungsKept;
    }
    // Tier 3: empty seed — gates decide.
    let trend_allows = !ctx.only_with_trend
        || match (ctx.trend, side) {
            (Some(t), Side::Long) => ctx.close > t,
            (Some(t), Side::Short) => ctx.close < t,
            (None, _) => false,
        };
    let allowed = !ctx.drawdown_breached && !ctx.chop_gate_active && trend_allows;
    if !allowed {
        return SeedOutcome::BlockedEmpty;
    }
    // Anchor: this bar's open, and only this bar's.
    let n = ctx.rung_count.max(1);
    let mut rungs = Vec::with_capacity(n as usize);
    for k in 1..=n {
        let offset = Money(ctx.step.0.saturating_mul(k as i64));
        let level = match side {
            Side::Long => Money(ctx.open.0.saturating_sub(offset.0)),
            Side::Short => Money(ctx.open.0.saturating_add(offset.0)),
        };
        rungs.push(Rung::armed(k, side, level, ctx.step));
    }
    *side_rungs_mut(state, side) = rungs;
    match side {
        Side::Long => state.long_base = ctx.open,
        Side::Short => state.short_base = ctx.open,
    }
    SeedOutcome::Seeded
}

/// Convenience: seed both sides for one bar, mirroring TS's per-bar loop
/// which calls `seedLadderSide` for long then short.
pub fn seed_bar(state: &mut LadderState, ctx: &SeedContext) -> (SeedOutcome, SeedOutcome) {
    let long = seed_side(state, Side::Long, ctx);
    let short = seed_side(state, Side::Short, ctx);
    (long, short)
}

// ---------------------------------------------------------------------------
// Slice 3: rung sizing. Port of TS `ladderRungQty`
// (ladder-engine.ts:1243-1339) + `orderableQty` (types.ts:31-48).
//
// Four traps are reproduced deliberately, each a structural divergence rather
// than a rounding detail (see docs/plans/ladder-port-spec.md):
//   1. specs-absent returns raw with NO orderableQty and NO margin check
//   2. two skips fire only AFTER orderableQty raises qty to minQty
//   3. raw = min(marginSized, notionalSized); at the soak's runtime values
//      the two are equal, so this is a tie and neither term binds
//   4. orderableQty ceils to qtyStep, floors back only if the ceiled qty
//      exceeds cap, then UNCONDITIONALLY raises to minQty — which is exactly
//      what makes trap 2's skips reachable
// ---------------------------------------------------------------------------

/// Exchange contract sizing constraints. Mirrors TS `ContractSizeSpec`.
///
/// **Units are BASE-ASSET MICROS**, unlike TS where `minQty`/`qtyStep` are
/// whole-asset floats (`0.001` SOL). The adapter converts once, at the
/// boundary, via `x * 1_000_000`, so all math in this module stays integer.
///
/// `None` when the bybit instrument fetch fails or the resolved contract is
/// malformed (`bybitContractSpecs` returns undefined,
/// ladder-engine.ts:3629-3647), so the specs-absent branch is a real runtime
/// state and not a defensive fallback.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ContractSpec {
    pub min_qty: i64,
    pub qty_step: i64,
}

/// Why a rung was not sized. Mirrors TS's `skipReason` strings so a monitor
/// reading either runtime sees the same diagnosis.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SkipReason {
    PerRungAllocationZero,
    NonPositiveFillPrice,
    /// The min-orderable floor pushed notional past the per-rung cap.
    MinNotionalExceedsMargin {
        notional: Money,
        margin: Money,
        leverage: i64,
        cap: Money,
    },
    /// The min-orderable floor pushed notional past the notional cap share.
    MinNotionalExceedsCap {
        notional: Money,
        cap_share: Money,
    },
}

/// Sizing outcome. `qty` is base-asset micros, matching `qty_base_micros`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RungQty {
    pub qty: i64,
    pub leverage: i64,
    pub skip: Option<SkipReason>,
}

/// Sizing inputs, mirroring the `LadderPaperTradingOptions` fields the TS path
/// actually reads.
#[derive(Debug, Clone, Copy)]
pub struct SizingOptions {
    /// `maxPositionPct` as percent (0-100). Runtime value for the soak is 100
    /// (partition-expanded), not the CLI's 50.
    pub max_position_pct: i64,
    /// `maxNotionalPct` as percent, or `None` when unset. Hardcoded to 100 on
    /// every ladder call, so `Some(100)` is the soak's value.
    pub max_notional_pct: Option<i64>,
    /// `rungs` — from the whitelist row, not a CLI flag.
    pub rungs: i64,
    /// Requested leverage. Ignored when `fully_dynamic` is true.
    pub leverage: i64,
    /// When true, leverage is the account-scaled cap and `leverage` is ignored.
    pub fully_dynamic: bool,
    /// Ceiling from `NEURATRADE_MAX_LADDER_LEVERAGE` (default 10).
    pub max_leverage: i64,
    /// Contract sizing, or `None` when specs did not resolve.
    pub spec: Option<ContractSpec>,
    /// Maker fee in BASIS POINTS. TS's `feePct` is a percent (0.05 = 5bp), so
    /// the caller converts once: `feePct * 100`.
    pub maker_fee_bp: i64,
    /// Taker exit fee in bp; falls back to `maker_fee_bp` (TS `takerExitFeePct`).
    pub taker_exit_fee_bp: Option<i64>,
    /// Live-entry cross cost in bps; 0 unless explicitly set.
    pub live_entry_cross_bps: i64,
}

/// Per-rung allocation: `capital * (maxPositionPct/100) / rungs`.
/// Mirrors TS `ladderRungSize` (ladder-engine.ts:1140-1154).
pub fn per_rung_allocation(capital: Money, opts: SizingOptions) -> Money {
    let rungs = opts.rungs.max(1);
    let frac = opts.max_position_pct.clamp(0, 100);
    Money(scale(capital.0, frac, 100) / rungs)
}

/// Ceil-then-floor-back contract rounding, then an unconditional minQty raise.
/// Port of TS `orderableQty` (types.ts:31-48).
///
/// The final minQty raise is what makes the two margin/cap skips reachable:
/// `qty` can exceed `cap` with no fallback, exactly as in TS.
pub fn orderable_qty(raw_qty: i64, spec: ContractSpec, fill_price: Money, cap: Money) -> i64 {
    if raw_qty <= 0 || spec.qty_step <= 0 {
        return raw_qty.max(0);
    }
    let step = spec.qty_step;
    let up = ceil_div(raw_qty, step) * step;
    let qty = if notional_usdt(up, fill_price) <= cap {
        up
    } else {
        (raw_qty / step) * step
    };
    qty.max(spec.min_qty)
}

/// TS `ladderRungQty` (ladder-engine.ts:1243-1339).
pub fn rung_qty(capital: Money, opts: SizingOptions, fill_price: Money) -> RungQty {
    let alloc = per_rung_allocation(capital, opts);
    let leverage_floor = opts.leverage.max(1);
    if alloc.0 <= 0 {
        return RungQty {
            qty: 0,
            leverage: leverage_floor,
            skip: Some(SkipReason::PerRungAllocationZero),
        };
    }
    if fill_price.0 <= 0 {
        return RungQty {
            qty: 0,
            leverage: leverage_floor,
            skip: Some(SkipReason::NonPositiveFillPrice),
        };
    }
    let cap = account_scaled_leverage_cap(
        capital,
        opts.max_position_pct.clamp(0, 100) * 100,
        opts.max_leverage,
    );
    // `raw` is BASE micros (TS: Decimal.div gives base units), so USDT micros
    // must be scaled by 1e6 before dividing by the price micros.
    let margin_sized = scale(alloc.0, 1_000_000, fill_price.0);
    let notional_sized = match opts.max_notional_pct {
        None => margin_sized,
        Some(pct) => {
            let share = scale(capital.0, pct.clamp(0, 100), 100) / opts.rungs.max(1);
            scale(share, 1_000_000, fill_price.0)
        }
    };
    let raw = margin_sized.min(notional_sized);

    // Trap 1: specs absent is structurally different — no rounding, no skips.
    let Some(spec) = opts.spec else {
        let lev = dynamic_leverage(notional_usdt(raw, fill_price), alloc, opts, cap);
        return RungQty {
            qty: raw.max(0),
            leverage: lev,
            skip: None,
        };
    };

    let qty = orderable_qty(raw, spec, fill_price, alloc);
    let notional = notional_usdt(qty, fill_price);
    let leverage = dynamic_leverage(notional, alloc, opts, cap);
    let lev = leverage.max(1);
    // Compare in NOTIONAL space: `notional > alloc * leverage` is exactly TS's
    // `notional/leverage > alloc` with no truncating division, so a rung whose
    // margin sits exactly on the cap reads the same way TS reads it.
    let margin = Money(notional.0 / lev);
    let margin_exceeds = notional.0 > alloc.0.saturating_mul(lev);

    // Trap 2a: minQty raise pushed margin past the per-rung cap.
    if margin_exceeds {
        return RungQty {
            qty: 0,
            leverage: lev,
            skip: Some(SkipReason::MinNotionalExceedsMargin {
                notional,
                margin,
                leverage: lev,
                cap: alloc,
            }),
        };
    }
    // Trap 2b: same raise pushed notional past the cap share.
    if let Some(pct) = opts.max_notional_pct {
        let cap_share = Money(scale(capital.0, pct.clamp(0, 100), 100) / opts.rungs.max(1));
        if notional > cap_share {
            return RungQty {
                qty: 0,
                leverage,
                skip: Some(SkipReason::MinNotionalExceedsCap {
                    notional,
                    cap_share,
                }),
            };
        }
    }
    RungQty {
        qty,
        leverage,
        skip: None,
    }
}

/// TS `dynamicLeverage` (ladder-engine.ts:1198-1227).
fn dynamic_leverage(raw_notional: Money, alloc: Money, opts: SizingOptions, cap: i64) -> i64 {
    let cap = cap.max(1);
    if opts.fully_dynamic {
        return cap.max(1);
    }
    let requested = opts.leverage.max(1);
    if alloc.0 <= 0 || raw_notional.0 <= 0 {
        return requested;
    }
    let needed = ceil_div(raw_notional.0, alloc.0);
    cap.min(requested.max(needed)).max(1)
}

/// Base micros -> USDT micros (the inverse of the `raw` scaling above).
fn notional_usdt(qty_base_micros: i64, fill_price: Money) -> Money {
    Money(scale(qty_base_micros, fill_price.0, 1_000_000))
}

fn ceil_div(a: i64, b: i64) -> i64 {
    if b <= 0 {
        return 0;
    }
    let q = a / b;
    if a % b != 0 && (a > 0) == (b > 0) {
        q + 1
    } else {
        q
    }
}

/// Integer scale, matching the engine's existing helper.
fn scale(a: i64, num: i64, denom: i64) -> i64 {
    ((a as i128 * num as i128) / denom as i128) as i64
}

/// `scale` rounded to nearest rather than truncated. Used where a value is
/// converted into `Money` micros and truncation would compound.
fn scale_round(a: i64, num: i64, denom: i64) -> i64 {
    let prod = a as i128 * num as i128;
    let d = denom as i128;
    if prod >= 0 {
        ((prod + d / 2) / d) as i64
    } else {
        ((prod - d / 2) / d) as i64
    }
}

// ---------------------------------------------------------------------------
// Slice 4: the per-bar tick loop. Port of TS `advanceLadderBar`
// (ladder-engine.ts:949-1004) + `fillLadderSide` (:638-670) +
// `closeLadderTargets` (:833-885).
//
// Mirrors TS's signature: state and options are SEPARATE parameters
// (`advanceLadderBar(w, candles, i, opts)`) — `opts` is caller-supplied per
// invocation, `state` is what gets persisted. `SizingOptions` therefore
// threads alongside `&mut LadderState`, never inside it.
// ---------------------------------------------------------------------------

/// One OHLCV bar. TS `CandleLike` is `open/high/low/close` + `timestamp`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Candle {
    pub open: Money,
    pub high: Money,
    pub low: Money,
    pub close: Money,
    /// Bar open time, ms epoch.
    pub ts_ms: i64,
}

/// A rung filled on this bar. Mirrors TS `LadderFillEvent`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FillEvent {
    pub rung_index: u32,
    pub side: Side,
    /// Post-slippage entry price used by the paper ledger.
    pub fill_price: Money,
    /// Raw grid level, pre-slippage.
    pub level: Money,
    /// Orderable qty in base micros, resolved at fill time.
    pub qty: i64,
}

/// Why a rung closed. Mirrors TS's four reasons.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CloseReason {
    Target,
    Stop,
    Liquidation,
    MaxHold,
}

/// A rung closed on this bar. Mirrors TS `LadderCloseEvent`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CloseEvent {
    pub rung_index: u32,
    pub side: Side,
    pub entry_price: Money,
    pub exit_price: Money,
    pub reason: CloseReason,
    pub capital_before: Money,
    pub capital_after: Money,
    pub pnl: Money,
    /// The rung's persisted filled qty — the exact size the close sends.
    pub qty: i64,
    pub entry_ts_ms: i64,
    pub closed_ts_ms: i64,
}

/// One bar's events. Mirrors TS `LadderBarEvents`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct BarEvents {
    pub fills: Vec<FillEvent>,
    pub closes: Vec<CloseEvent>,
}

/// Per-bar context, mirroring TS `LadderBarContext`'s derived knobs.
#[derive(Debug, Clone, Copy)]
pub struct BarContext {
    pub bar_index: u64,
    pub candle: Candle,
    /// Current bar's step: `open * gridStepPct/100` — re-derived EVERY bar
    /// (TS `createLadderBarContext`), unlike `Rung::step` which is frozen at
    /// seed time.
    pub step: Money,
    /// Slippage as a multiplier in 1e4: 10000 = no slippage.
    pub slippage_bp: i64,
    pub rung_count: u32,
    pub target_ratio_x100: i64,
    pub max_hold_bars: i64,
    pub ms_per_bar: i64,
    pub conservative_intrabar: bool,
    /// TS `opts.maxDrawdownPct` — NOT persisted (the 10-field fingerprint
    /// does not include it; TS reads it off per-invocation opts). 0 or >=100
    /// disables the drawdown kill and the flat re-anchor, matching TS's
    /// `accountDrawdownBreached` guards.
    pub max_drawdown_pct: i64,
}

/// Fee schedule. TS reads `feePct` (maker), `takerExitFeePct` (falls back to
/// `feePct`), and `liveEntryCrossBps` (0 unless explicitly set), then forms
/// `targetFee = maker*2 + cross` and `stopFee = maker + taker + cross`.
/// Stored in basis points (1bp = 0.01%).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FeeSchedule {
    pub maker_bp: i64,
    pub taker_bp: i64,
    pub cross_bp: i64,
    /// Leverage multiplier applied to the net return.
    pub leverage: i64,
}

impl FeeSchedule {
    pub fn from_options(opts: &SizingOptions) -> Self {
        FeeSchedule {
            maker_bp: opts.maker_fee_bp,
            taker_bp: opts.taker_exit_fee_bp.unwrap_or(opts.maker_fee_bp),
            cross_bp: opts.live_entry_cross_bps,
            // In fully-dynamic mode the engine's cap is applied at sizing
            // time and the return is NOT multiplied by it here — TS's
            // `closeRung` uses `opts.leverage`, which the fully-dynamic path
            // leaves at its requested value.
            leverage: opts.leverage.max(1),
        }
    }
}

/// Advance the ladder by one bar: pause decay, drawdown re-anchor, re-seed
/// flat sides, fill touched rungs, then whole-side risk exits or per-rung
/// target/max-hold closes.
///
/// Deliberately NOT ported yet (each named, per the spec's pattern): the chop
/// gate and trend filter (both 0 in the soak config), liquidation (dead at
/// leverage 1) and `maxPositionDrawdownPct` (slice 6). The stop boundary and
/// account drawdown kill ARE ported — see `resolve_risk_exit`.
pub fn advance_bar(state: &mut LadderState, ctx: &BarContext, opts: SizingOptions) -> BarEvents {
    let mut events = BarEvents::default();

    // Pause decay happens before anything else and short-circuits the bar.
    if state.paused > 0 {
        state.paused -= 1;
        return events;
    }

    // Drawdown-peak re-anchor: a flat account blocked by realized drawdown can
    // never trade its way back, so the kill latched forever with no operator
    // reset path (TS comment, ENA shadow 2026-09-03: a +2.64 book went
    // permanently silent after an 8% peak-to-capital slide). Re-anchor peak
    // to capital and take the pause instead. Condition is TS's threshold
    // (`accountDrawdownBreached`, peak-to-capital slide >= maxDrawdownPct),
    // NOT "any dip": below the cap TS keeps the old peak and keeps seeding.
    let any_filled = state.rungs(Side::Long).iter().any(|r| r.filled)
        || state.rungs(Side::Short).iter().any(|r| r.filled);
    if !any_filled
        && account_drawdown_breached(state.peak_capital, state.capital, ctx.max_drawdown_pct)
    {
        state.peak_capital = state.capital;
        state.paused = state.config.grid_pause_after_loss_bars.max(0) as u64;
    }

    let seed_ctx = SeedContext {
        open: ctx.candle.open,
        close: ctx.candle.close,
        step: ctx.step,
        trend: None,
        only_with_trend: state.config.only_with_trend,
        chop_gate_active: false,
        drawdown_breached: account_drawdown_breached(
            state.peak_capital,
            state.capital,
            ctx.max_drawdown_pct,
        ),
        rung_count: ctx.rung_count,
    };
    let _ = seed_side(state, Side::Long, &seed_ctx);
    let _ = seed_side(state, Side::Short, &seed_ctx);

    let fees = FeeSchedule::from_options(&opts);
    manage_side(state, ctx, opts, &fees, Side::Long, &mut events);
    manage_side(state, ctx, opts, &fees, Side::Short, &mut events);

    state.bars_consumed += 1;
    state.last_ts_ms = Some(ctx.candle.ts_ms);
    events
}

/// TS `manageLadderSide` (ladder-engine.ts:892-904): fill, then whole-side
/// risk exits (stop boundary / account drawdown), else per-rung closes. The
/// risk exit runs BEFORE the target loop and has no same-bar gate: a rung
/// filled on this very bar is closed by the boundary in the same pass
/// (boundary fixture, bar 2).
fn manage_side(
    state: &mut LadderState,
    ctx: &BarContext,
    opts: SizingOptions,
    fees: &FeeSchedule,
    side: Side,
    events: &mut BarEvents,
) {
    if state.rungs(side).is_empty() {
        return;
    }
    fill_side(state, ctx, opts, side, events);
    let filled: Vec<Rung> = state
        .rungs(side)
        .iter()
        .copied()
        .filter(|r| r.filled)
        .collect();
    if filled.is_empty() {
        return;
    }
    if let Some(exit) = resolve_risk_exit(state, ctx, side, &filled) {
        apply_risk_exit(state, ctx, side, &filled, exit, fees, events);
        return;
    }
    close_targets(state, ctx, fees, side, events);
}

/// TS `fillLadderSide` (:638-670): progressive fill on touch.
///
/// Two guards carried exactly: a rung fills only if the PREVIOUS rung on that
/// side is filled (or it is index 0), and a floor-unorderable size is a clean
/// HOLD — the rung is not marked filled and no event is emitted, so paper and
/// live stay aligned.
fn fill_side(
    state: &mut LadderState,
    ctx: &BarContext,
    opts: SizingOptions,
    side: Side,
    events: &mut BarEvents,
) {
    let count = state.rungs(side).len();
    for index in 0..count {
        let prev_filled = index == 0 || state.rungs(side)[index - 1].filled;
        if !prev_filled {
            continue;
        }
        let rung = state.rungs(side)[index];
        if rung.filled || !level_touched(side, &ctx.candle, rung.level) {
            continue;
        }
        // TS uses asymmetric slippage here: `level * slippage` for longs and
        // `level / slippage` for shorts — NOT nt_grid's symmetric rule.
        let fill_price = if side == Side::Long {
            Money(scale(rung.level.0, 10_000 + ctx.slippage_bp, 10_000))
        } else {
            Money(scale(rung.level.0, 10_000, 10_000 + ctx.slippage_bp))
        };
        let sized = rung_qty(state.capital, opts, fill_price);
        if sized.qty <= 0 || sized.skip.is_some() {
            continue;
        }
        state.rungs_mut(side)[index] = Rung {
            filled: true,
            entry_price: fill_price,
            entry_bar: state.bars_consumed,
            entry_ts_ms: ctx.candle.ts_ms,
            filled_qty: sized.qty,
            ..rung
        };
        events.fills.push(FillEvent {
            rung_index: rung.rung_index,
            side,
            fill_price,
            level: rung.level,
            qty: sized.qty,
        });
    }
}

/// One whole-side risk exit (TS `LadderRiskExit`, :550-556). Both ported
/// branches close ALL filled rungs and reset the side; the partial branch
/// (`maxPositionDrawdownPct`, `resetAll: false`) arrives with slice 6.
struct RiskExit {
    exit_price: Money,
    reason: CloseReason,
    pause_after_loss: bool,
}

/// TS `resolveLadderRiskExit` (:734-793), evaluated in TS's order. Ported:
/// (1) stop boundary — legacy and `stopRatio > 0`; (2) account drawdown kill.
/// NOT ported, each named: liquidation (`sideLiquidationLevel`, dead at
/// leverage 1, slice 6) and `maxPositionDrawdownPct` (slice 6).
///
/// **Stop boundary anchoring.** TS uses the EXIT bar's step for BOTH forms
/// (:681-682 ratio, :685-686 legacy). The `stopRatio > 0` form here uses the
/// rung's FROZEN entry-time step instead — the named `LADDER-STEP-ANCHOR`
/// deviation (port spec §4): a risk level must not drift with price after
/// entry, the same class of bug `a07b3dd0` removed from seeding. The legacy
/// form stays TS-exact on the exit-bar step because the boundary fixture's
/// `93.049` pin depends on it.
fn resolve_risk_exit(
    state: &LadderState,
    ctx: &BarContext,
    side: Side,
    filled: &[Rung],
) -> Option<RiskExit> {
    // 1. Stop boundary: long stops under the lowest entry, short above the
    //    highest (TS min/max over entries, :679-682).
    let boundary = if state.config.stop_ratio_x100 > 0 {
        let ratio = state.config.stop_ratio_x100;
        match side {
            Side::Long => {
                let r = filled.iter().min_by_key(|r| r.entry_price.0)?;
                Money(r.entry_price.0 - scale(r.step.0, ratio, 100))
            }
            Side::Short => {
                let r = filled.iter().max_by_key(|r| r.entry_price.0)?;
                Money(r.entry_price.0 + scale(r.step.0, ratio, 100))
            }
        }
    } else {
        // Legacy: sideBase ± ctx.step * (rungCount + gridMaxGrids), TS-exact.
        let span = scale(
            ctx.step.0,
            ctx.rung_count as i64 + state.config.grid_max_grids,
            1,
        );
        match side {
            Side::Long => Money(state.base(side).0 - span),
            Side::Short => Money(state.base(side).0 + span),
        }
    };
    let touched = match side {
        Side::Long => ctx.candle.low <= boundary,
        Side::Short => ctx.candle.high >= boundary,
    };
    if touched {
        return Some(RiskExit {
            exit_price: side_exit_price(side, boundary, ctx.slippage_bp),
            reason: CloseReason::Stop,
            pause_after_loss: true,
        });
    }

    // 2. Account drawdown kill: whole side out at the bar close. No pause —
    //    the flat re-anchor in `advance_bar` takes the pause instead (TS
    //    `pauseAfterLoss: false`, :788-790).
    if account_drawdown_breached(state.peak_capital, state.capital, ctx.max_drawdown_pct) {
        return Some(RiskExit {
            exit_price: side_exit_price(side, ctx.candle.close, ctx.slippage_bp),
            reason: CloseReason::Stop,
            pause_after_loss: false,
        });
    }
    None
}

/// TS `applyLadderRiskExit` (:795-831), `resetAll` branch: close every
/// filled rung in order at the exit price, clear the side + base, then take
/// the post-loss pause when configured.
fn apply_risk_exit(
    state: &mut LadderState,
    ctx: &BarContext,
    side: Side,
    filled: &[Rung],
    exit: RiskExit,
    fees: &FeeSchedule,
    events: &mut BarEvents,
) {
    for rung in filled {
        close_rung(
            state,
            ctx,
            CloseRequest {
                side,
                rung: *rung,
                exit_price: exit.exit_price,
                reason: exit.reason,
                fees,
            },
            events,
        );
    }
    *state.rungs_mut(side) = Vec::new();
    match side {
        Side::Long => state.long_base = Money::ZERO,
        Side::Short => state.short_base = Money::ZERO,
    }
    if exit.pause_after_loss && state.config.grid_pause_after_loss_bars > 0 {
        state.paused = state.config.grid_pause_after_loss_bars as u64;
    }
}

/// TS `closeLadderTargets` (:833-885): per-rung take-profit, else max-hold.
///
/// Target uses the rung's FROZEN `step` (seed-time), not the bar's current
/// step — the LADDER-STEP-ANCHOR divergence the spec records. `can_close_on_
/// same_bar` is the conservative-intrabar rule: a rung cannot target-exit on
/// the bar it filled unless the config explicitly opts out.
fn close_targets(
    state: &mut LadderState,
    ctx: &BarContext,
    fees: &FeeSchedule,
    side: Side,
    events: &mut BarEvents,
) {
    let mut still_open: Vec<Rung> = Vec::new();
    let mut any_closed = false;
    let rungs = state.rungs(side).to_vec();
    for rung in rungs {
        if !rung.filled {
            still_open.push(rung);
            continue;
        }
        let target = if side == Side::Long {
            Money(rung.entry_price.0 + scale(rung.step.0, ctx.target_ratio_x100, 100))
        } else {
            Money(rung.entry_price.0 - scale(rung.step.0, ctx.target_ratio_x100, 100))
        };
        let target_touched = if side == Side::Long {
            ctx.candle.high >= target
        } else {
            ctx.candle.low <= target
        };
        let can_close_same_bar = !ctx.conservative_intrabar || rung.entry_bar < ctx.bar_index;
        if target_touched && can_close_same_bar {
            let exit = if side == Side::Long {
                Money(scale(target.0, 10_000, 10_000 + ctx.slippage_bp))
            } else {
                Money(scale(target.0, 10_000 + ctx.slippage_bp, 10_000))
            };
            close_rung(
                state,
                ctx,
                CloseRequest {
                    side,
                    rung,
                    exit_price: exit,
                    reason: CloseReason::Target,
                    fees,
                },
                events,
            );
            any_closed = true;
            continue;
        }
        let held = ctx.candle.ts_ms.saturating_sub(rung.entry_ts_ms);
        if ctx.max_hold_bars > 0
            && rung.entry_ts_ms > 0
            && held >= ctx.max_hold_bars.saturating_mul(ctx.ms_per_bar)
        {
            let exit = side_exit_price(side, ctx.candle.close, ctx.slippage_bp);
            close_rung(
                state,
                ctx,
                CloseRequest {
                    side,
                    rung,
                    exit_price: exit,
                    reason: CloseReason::MaxHold,
                    fees,
                },
                events,
            );
            any_closed = true;
            continue;
        }
        still_open.push(rung);
    }
    *state.rungs_mut(side) = still_open;
    if any_closed && state.rungs(side).iter().all(|r| !r.filled) {
        *state.rungs_mut(side) = Vec::new();
        if side == Side::Long {
            state.long_base = Money::ZERO;
        } else {
            state.short_base = Money::ZERO;
        }
    }
}

/// One rung close, bundled so `close_rung` stays within the arg limit.
#[derive(Debug, Clone, Copy)]
struct CloseRequest<'a> {
    side: Side,
    rung: Rung,
    exit_price: Money,
    reason: CloseReason,
    fees: &'a FeeSchedule,
}

/// TS `closeRung` (ladder-engine.ts:~285-345): apply the return fraction to
/// capital, drop the rung, emit the event.
///
/// **PnL is a RETURN FRACTION, not a qty-times-price amount.** TS computes
/// `pricePnl = (exit - entry) / entry` (long) or `(entry - exit) / entry`
/// (short), subtracts a fee, multiplies by leverage, then by
/// `sizePerRung = positionFraction / rungs`, and applies it MULTIPLICATIVELY:
/// `capital *= 1 + equityReturn`. So the rung's `filled_qty` does not enter
/// the capital math at all — it is only the size the live executor sends.
/// A qty-times-price implementation diverges immediately.
///
/// Fees mirror TS `closeRung` (:301-310): `target` exits pay
/// `maker*2 + cross`, everything else `maker + taker + cross`. Funding cost
/// is not ported (slice 6).
fn close_rung(
    state: &mut LadderState,
    ctx: &BarContext,
    close: CloseRequest,
    events: &mut BarEvents,
) {
    let CloseRequest {
        side,
        rung,
        exit_price,
        reason,
        fees,
    } = close;
    let capital_before = state.capital;
    let price_pnl_num = if side == Side::Long {
        exit_price.0 - rung.entry_price.0
    } else {
        rung.entry_price.0 - exit_price.0
    };
    // pricePnl is (exit - entry)/entry. Both are micros, so the ratio is
    // already dimensionless; scale it to 1e18 fixed point so a 5bp fee
    // subtracts exactly rather than truncating.
    let price_pnl = scale(
        price_pnl_num,
        1_000_000_000_000_000_000,
        rung.entry_price.0.max(1),
    );
    let is_liquidation = reason == CloseReason::Liquidation;
    let fee = if is_liquidation {
        0
    } else if reason == CloseReason::Target {
        fees.maker_bp * 2 + fees.cross_bp
    } else {
        fees.maker_bp + fees.taker_bp + fees.cross_bp
    };
    let net = price_pnl - scale(fee, 1_000_000_000_000_000_000, 10_000);
    let leveraged = if is_liquidation {
        -1_000_000_000_000_000_000
    } else {
        net * fees.leverage
    };
    // `leveraged` is already in 1e18; sizePerRung is positionFraction/rungs,
    // and positionFraction is 1 here, so this is a plain divide.
    let equity_return = if is_liquidation {
        -scale(1_000_000_000_000_000_000, 1, ctx.rung_count.max(1) as i64)
    } else {
        leveraged / ctx.rung_count.max(1) as i64
    };
    // capital *= (1 + equity_return), floored at 0 like TS's Decimal.max.
    // Round rather than truncate: `Money` is micro-USDT, and truncating twice
    // per round-trip compounds to ~1.2e-6 on the 6-bar fixture, which exceeds
    // the representation's own ceiling.
    let next = scale_round(
        capital_before.0,
        1_000_000_000_000_000_000 + equity_return,
        1_000_000_000_000_000_000,
    );
    state.capital = Money(next.max(0));
    if is_liquidation || net < 0 {
        state.total_losses += 1;
    } else {
        state.total_wins += 1;
    }
    if state.capital > state.peak_capital {
        state.peak_capital = state.capital;
    }
    let pnl = Money(state.capital.0 - capital_before.0);
    events.closes.push(CloseEvent {
        rung_index: rung.rung_index,
        side,
        entry_price: rung.entry_price,
        exit_price,
        reason,
        capital_before,
        capital_after: state.capital,
        pnl,
        qty: rung.filled_qty,
        entry_ts_ms: rung.entry_ts_ms,
        closed_ts_ms: ctx.candle.ts_ms,
    });
}

/// TS `accountDrawdownBreached` (ladder-engine.ts:519-528): peak-to-capital
/// slide >= cap. Cap `<= 0` or `>= 100` disables; non-positive peak disables.
fn account_drawdown_breached(peak: Money, capital: Money, max_drawdown_pct: i64) -> bool {
    let cap = max_drawdown_pct;
    if cap <= 0 || cap >= 100 || peak.0 <= 0 {
        return false;
    }
    ((peak.0 - capital.0) as i128) * 100 >= (peak.0 as i128) * cap as i128
}

/// TS `sideExitPrice` (ladder-engine.ts:708-717): MULTIPLICATIVE slip —
/// `long: p*(1-slippage)`, `short: p*(1+slippage)`. This is a different
/// function from TS's target exit (`target / slippage`, :856): max-hold and
/// the whole-side risk exits use this one. Was the division form here, which
/// is wrong for those paths by O(slip^2) — the same class as nt-grid's
/// recorded short-leg slippage debt; at slippage 0 both forms coincide.
fn side_exit_price(side: Side, price: Money, slippage_bp: i64) -> Money {
    if side == Side::Long {
        Money(scale(price.0, 10_000 - slippage_bp, 10_000))
    } else {
        Money(scale(price.0, 10_000 + slippage_bp, 10_000))
    }
}

/// TS `sideLevelTouched`: a long rung fills when price falls TO the level, a
/// short rung when price rises TO it.
fn level_touched(side: Side, candle: &Candle, level: Money) -> bool {
    match side {
        Side::Long => candle.low <= level,
        Side::Short => candle.high >= level,
    }
}
