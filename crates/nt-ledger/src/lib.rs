//! Append-only fill ledger with running totals.
//! Position-aware PnL arrives with the grid engine (P5 Step 3);
//! this crate only records what execution filled, in integer micros.

use nt_execution::Fill;
use nt_risk::Money;

/// Running totals over recorded fills.
#[derive(Debug, Default, Clone, Copy, PartialEq, Eq)]
pub struct Totals {
    pub fills: u64,
    pub gross_micros: i64,
    pub fees_micros: i64,
}

impl Totals {
    pub fn net_micros(self) -> i64 {
        self.gross_micros - self.fees_micros
    }
}

/// Append-only record of fills.
#[derive(Debug, Default)]
pub struct Ledger {
    fills: Vec<Fill>,
    totals: Totals,
}

impl Ledger {
    pub fn new() -> Ledger {
        Ledger::default()
    }

    /// Record a fill. Gross sums signed pre-fee cash flow; `net` backs fees out.
    /// Uses saturating adds: a totals overflow pins instead of wrapping.
    /// Memory note: this is a process-local window, not a store — fills must
    /// dual-write to Postgres (P3) and the Vec stays bounded by session length.
    pub fn apply(&mut self, fill: Fill) {
        self.totals.fills += 1;
        self.totals.gross_micros = self
            .totals
            .gross_micros
            .saturating_add(fill.proceeds_micros.0);
        self.totals.fees_micros = self.totals.fees_micros.saturating_add(fill.fee_micros.0);
        self.fills.push(fill);
    }

    pub fn totals(&self) -> Totals {
        self.totals
    }

    pub fn len(&self) -> usize {
        self.fills.len()
    }

    pub fn is_empty(&self) -> bool {
        self.fills.is_empty()
    }

    pub fn gross_render(&self) -> String {
        Money(self.totals.gross_micros).render()
    }
}
