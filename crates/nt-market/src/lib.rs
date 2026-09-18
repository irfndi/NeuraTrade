//! Market data primitives for the paper grid engine (P5 Step 3).
//! Candles only — no exchange I/O here; sync stays in the TS soak until P3.

use nt_risk::Money;

/// One OHLCV candle. Prices in micro-USDT, `open_ts_ms` UTC.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Candle {
    pub open_ts_ms: i64,
    pub open: Money,
    pub high: Money,
    pub low: Money,
    pub close: Money,
    pub volume_base_micros: i64,
}

/// Oldest-first panel. Only closed candles feed the engine; the forming
/// candle is excluded by callers (same rule as the TS soak's `no new candle`).
#[derive(Debug, Default, Clone)]
pub struct Panel {
    candles: Vec<Candle>,
}

impl Panel {
    pub fn new(mut candles: Vec<Candle>) -> Panel {
        candles.sort_by_key(|c| c.open_ts_ms);
        Panel { candles }
    }

    pub fn len(&self) -> usize {
        self.candles.len()
    }

    pub fn is_empty(&self) -> bool {
        self.candles.is_empty()
    }

    pub fn latest_close(&self) -> Option<Money> {
        self.candles.last().map(|c| c.close)
    }

    pub fn closes(&self) -> impl Iterator<Item = Money> + '_ {
        self.candles.iter().map(|c| c.close)
    }
}
