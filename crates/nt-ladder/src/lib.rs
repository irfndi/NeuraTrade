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
    st.long_rungs = Vec::new();
    st.short_rungs = Vec::new();

    let mut rung_lines = 0usize;
    for line in lines {
        let f: Vec<&str> = line.split_whitespace().collect();
        if f.is_empty() {
            continue;
        }
        match f.as_slice() {
            ["initial_capital", v] => st.initial_capital = Money(num(v)?),
            ["capital", v] => st.capital = Money(num(v)?),
            ["peak_capital", v] => st.peak_capital = Money(num(v)?),
            ["total_wins", v] => st.total_wins = num(v)? as u64,
            ["total_losses", v] => st.total_losses = num(v)? as u64,
            ["long_base", v] => st.long_base = Money(num(v)?),
            ["short_base", v] => st.short_base = Money(num(v)?),
            ["paused", v] => st.paused = num(v)? as u64,
            ["grid_step_bp", v] => st.config.grid_step_bp = num(v)?,
            ["grid_max_grids", v] => st.config.grid_max_grids = num(v)?,
            ["grid_pause_after_loss_bars", v] => st.config.grid_pause_after_loss_bars = num(v)?,
            ["rungs", v] => st.config.rungs = num(v)? as u32,
            ["target_ratio_x100", v] => st.config.target_ratio_x100 = num(v)?,
            ["only_with_trend", v] => st.config.only_with_trend = num(v)? != 0,
            ["chop_gate_adx", v] => st.config.chop_gate_adx = num(v)?,
            ["max_hold_bars", v] => st.config.max_hold_bars = num(v)?,
            ["stop_ratio_x100", v] => st.config.stop_ratio_x100 = num(v)?,
            ["conservative_intrabar", v] => st.config.conservative_intrabar = num(v)? != 0,
            ["last_ts", "none"] => st.last_ts_ms = None,
            ["last_ts", v] => st.last_ts_ms = Some(num(v)?),
            ["bars_consumed", v] => st.bars_consumed = num(v)? as u64,
            ["rung", index, side, level, step, filled, ep, ebar, ets, qty] => {
                rung_lines += 1;
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
            _ => return Err(ResumeError::BadRungLine(rung_lines + 1)),
        }
    }
    Ok(st)
}
