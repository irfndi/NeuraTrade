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
