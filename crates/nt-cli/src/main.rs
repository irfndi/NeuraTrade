// ponytail: shadow runner — replays closed-candle CSV through the native
// paper engine and prints ledger totals. Paper-only: no venue, no orders,
// no secrets. Input CSV columns: open_ts_ms,open,high,low,close micros.
// (Export with: sqlite3 demo.db ".headers on" "SELECT ..." > bars.csv)
use nt_grid::{PaperEngineConfig, run_paper_engine};
use nt_market::Candle;
use nt_risk::{Money, RiskLimits};
use std::env;

fn usage() -> ! {
    eprintln!(
        "usage: nt-cli shadow --bars <csv> [--capital-usdt N] [--fee-bp N] [--timeframe-ms N]"
    );
    std::process::exit(2);
}

fn main() {
    let args: Vec<String> = env::args().collect();
    match args.get(1).map(String::as_str) {
        Some("health") => {
            println!("nt-cli 0.1.0 status=ok runtime=rust-strangler");
            return;
        }
        Some("shadow") => {}
        _ => {
            if args.len() <= 1 {
                println!("nt-cli 0.1.0");
                return;
            }
            usage();
        }
    }
    let mut bars: Option<String> = None;
    let mut capital_usdt: i64 = 50;
    let mut fee_bp: i64 = nt_execution_bp();
    let mut timeframe_ms: Option<i64> = None;
    let mut i = 2;
    while i < args.len() {
        match args[i].as_str() {
            "--bars" => {
                i += 1;
                bars = Some(args.get(i).unwrap_or_else(|| usage()).clone());
            }
            "--capital-usdt" => {
                i += 1;
                capital_usdt = args
                    .get(i)
                    .unwrap_or_else(|| usage())
                    .parse()
                    .unwrap_or_else(|_| usage());
            }
            "--fee-bp" => {
                i += 1;
                fee_bp = args
                    .get(i)
                    .unwrap_or_else(|| usage())
                    .parse()
                    .unwrap_or_else(|_| usage());
            }
            "--timeframe-ms" => {
                i += 1;
                timeframe_ms = Some(
                    args.get(i)
                        .unwrap_or_else(|| usage())
                        .parse()
                        .unwrap_or_else(|_| usage()),
                );
            }
            _ => usage(),
        }
        i += 1;
    }
    let path = bars.unwrap_or_else(|| usage());
    let text = std::fs::read_to_string(&path).unwrap_or_else(|e| {
        eprintln!("read {path}: {e}");
        std::process::exit(1);
    });
    let mut candles = Vec::new();
    for (n, line) in text.lines().enumerate() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') || line.starts_with("open_ts_ms") {
            continue;
        }
        let f: Vec<&str> = line.split(',').collect();
        if f.len() < 5 {
            eprintln!("line {}: want 5 cols, got {}", n + 1, f.len());
            std::process::exit(1);
        }
        let num = |s: &str| -> i64 {
            s.trim().parse().unwrap_or_else(|_| {
                eprintln!("line {}: bad number {s:?}", n + 1);
                std::process::exit(1);
            })
        };
        candles.push(Candle {
            open_ts_ms: num(f[0]),
            open: Money(num(f[1])),
            high: Money(num(f[2])),
            low: Money(num(f[3])),
            close: Money(num(f[4])),
            volume_base_micros: 1_000_000,
        });
    }
    candles.sort_by_key(|c| c.open_ts_ms); // oldest-first contract for run_paper_engine
    // Closed-only (nt-market Panel contract): drop the forming bar. With
    // --timeframe-ms, a bar is closed iff now >= open_ts + timeframe; without
    // it, drop the newest row (a CSV export's tail is the forming candle).
    if let Some(tf) = timeframe_ms {
        let now_ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_millis() as i64)
            .unwrap_or(i64::MAX);
        candles.retain(|c| c.open_ts_ms.saturating_add(tf) <= now_ms);
    } else if candles.len() > 1 {
        candles.pop();
    }
    // Paper-engine geometry (mirrors paper_engine_check fixture scale):
    // step 1.00%, target 1.00x step, stop 2 grids, 50bps slippage,
    // 10% position. Fee per symbol via --fee-bp.
    let cfg = PaperEngineConfig {
        step_bp: 100,
        target_ratio_x100: 100,
        grid_max_grids: 2,
        slippage_bps: 50,
        max_position_size_pct: 10,
        fee_bp,
    };
    let capital = Money(capital_usdt * 1_000_000);
    let limits = RiskLimits {
        min_capital: Money(30 * 1_000_000),
        ..RiskLimits::live()
    };
    let (events, ledger) = run_paper_engine(&candles, &cfg, capital, &limits);
    let t = ledger.totals();
    println!(
        "shadow bars={} fills={} gross={} fees={} net={} events={}",
        candles.len(),
        t.fills,
        t.gross_micros,
        t.fees_micros,
        t.net_micros(),
        events.len()
    );
}

/// Honest taker-exit default (6bp) without depending on nt-execution
/// (which would pull the RiskApproval seal into a paper-only binary).
fn nt_execution_bp() -> i64 {
    6
}
