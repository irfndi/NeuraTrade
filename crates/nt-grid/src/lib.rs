//! Minimal rung-ladder signal core (P5 Step 3 start).
//! Rungs sit `step_bp` apart below/above an anchor; a close crossing a rung
//! emits one signal. Integer basis points throughout — no float.

use nt_market::Panel;
use nt_risk::Money;

pub mod engine;
pub mod sleeves;
pub use engine::{
    EndState, FillReason, PaperEngineConfig, PaperFillEvent, ResumePosition, ResumeState, Side,
    run_paper_engine, run_paper_engine_from,
};
pub use sleeves::{
    FilterKind, SleeveCfg, SleeveFillEvent, Vote, account_scaled_leverage_cap, combine_votes,
    conviction_leverage, filter_vote, run_sleeve_backtest,
};

/// Grid geometry. `step_bp`: rung spacing in basis points of anchor.
#[derive(Debug, Clone, Copy)]
pub struct GridConfig {
    pub step_bp: i64,
    pub rungs: usize,
}

/// One rung-crossing signal on a closed candle.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Signal {
    /// Candle index in the panel.
    pub at: usize,
    /// Signed rung crossed: +k (up through rung k) or -k (down through rung k).
    pub rung: i64,
    pub price: Money,
}

/// Evaluate closes against rungs anchored at the first close.
/// `anchor*(10000±k*step)/10000`, integer division.
/// Emits at most one signal per candle: the outermost rung newly crossed
/// beyond the previously touched extreme in that direction.
pub fn evaluate(cfg: &GridConfig, panel: &Panel) -> Vec<Signal> {
    let closes: Vec<Money> = panel.closes().collect();
    if closes.is_empty() || cfg.rungs == 0 || cfg.step_bp <= 0 {
        return Vec::new();
    }
    let anchor = closes[0].0;
    if anchor <= 0 {
        return Vec::new();
    }
    let level = |k: i64| anchor * (10_000 + k * cfg.step_bp) / 10_000;
    let mut out = Vec::new();
    let mut hi_hit: i64 = 0;
    let mut lo_hit: i64 = 0;
    for (i, c) in closes.iter().enumerate().skip(1) {
        // Up side: outermost rung at or below this close.
        let mut k = (c.0 * 10_000 / anchor - 10_000) / cfg.step_bp;
        if k > cfg.rungs as i64 {
            k = cfg.rungs as i64;
        }
        if k > hi_hit {
            hi_hit = k;
            out.push(Signal {
                at: i,
                rung: k,
                price: Money(level(k)),
            });
            continue;
        }
        // Down side: outermost rung at or above this close.
        let mut kd = (10_000 - c.0 * 10_000 / anchor) / cfg.step_bp;
        if kd > cfg.rungs as i64 {
            kd = cfg.rungs as i64;
        }
        if kd > lo_hit {
            lo_hit = kd;
            out.push(Signal {
                at: i,
                rung: -kd,
                price: Money(level(-kd)),
            });
        }
    }
    out
}
