// ponytail: shadow runner — replays closed-candle CSV through the native
// paper engine and prints ledger totals. Paper-only: no venue, no orders,
// no secrets. Input CSV columns: open_ts_ms,open,high,low,close micros.
// (Export with: sqlite3 demo.db ".headers on" "SELECT ..." > bars.csv)
// Live-shadow runs: pass --capital-usdt = per-symbol partition (e.g. 200/4
// = 50 for the 4-ticker demo) + per-symbol --fee-bp, else parity diffs are
// config artifacts, not engine divergence.
// --state is a tick cursor (last open_ts): each tick replays only newer
// candles instead of double-counting. --resume persists open position +
// equity state so an exit whose entry sat in a prior tick still fires;
// without it a fresh engine replays a partial window and drops those exits.
// --ledger appends this tick's fills as CSV. All three are opt-in file
// paths (default off); no network, no secrets.
use nt_grid::{
    EndState, PaperEngineConfig, ResumePosition, ResumeState, Side, run_paper_engine_from,
};
use nt_market::Candle;
use nt_risk::{Money, RiskLimits};
use std::env;

fn usage() -> ! {
    eprintln!(
        "usage: nt-cli shadow --bars <csv> [--capital-usdt N] [--fee-bp N] [--timeframe-ms N] [--state <file>] [--resume <file>] [--ledger <file>]"
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
    let mut state_path: Option<String> = None;
    let mut resume_path: Option<String> = None;
    let mut ledger_path: Option<String> = None;
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
            "--state" => {
                i += 1;
                state_path = Some(args.get(i).unwrap_or_else(|| usage()).clone());
            }
            "--resume" => {
                i += 1;
                resume_path = Some(args.get(i).unwrap_or_else(|| usage()).clone());
            }
            "--ledger" => {
                i += 1;
                ledger_path = Some(args.get(i).unwrap_or_else(|| usage()).clone());
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
    // Closed-only (nt-market Panel contract): with --timeframe-ms a bar is
    // closed iff now >= open_ts + timeframe; without it, drop the newest row
    // (a CSV export's tail is the forming candle). Never both.
    if let Some(tf) = timeframe_ms {
        let now_ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_millis() as i64)
            .unwrap_or(i64::MAX);
        candles.retain(|c| c.open_ts_ms.saturating_add(tf) <= now_ms);
    } else if candles.len() > 1 {
        candles.pop();
    }
    // Incremental walk (TS forwardOnly parity): --state persists last open_ts
    // so each tick replays only newer candles instead of double-counting.
    if let Some(sp) = &state_path {
        let since: i64 = std::fs::read_to_string(sp)
            .ok()
            .and_then(|s| s.trim().parse().ok())
            .unwrap_or(i64::MIN);
        candles.retain(|c| c.open_ts_ms > since);
        if candles.is_empty() {
            println!("shadow bars=0 fills=0 gross=0 fees=0 net=0 events=0");
            return;
        }
    }
    // --resume persists open position + equity state so exits whose entries
    // sat in a prior tick still fire (see file header). Opt-in; default off.
    let resume: ResumeState = match &resume_path {
        None => ResumeState::default(),
        Some(rp) => load_resume(rp),
    };
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
    let (events, ledger, end) = run_paper_engine_from(&candles, &cfg, capital, &limits, &resume);
    // Cumulative view: prior ticks' totals + this tick's ledger, so `shadow`
    // prints continuous-equivalent PnL (ledger only holds this tick).
    let t = ledger.totals();
    println!(
        "shadow bars={} fills={} gross={} fees={} net={} events={}",
        resume.bar_offset.saturating_add(candles.len()),
        end.cum_fills,
        end.cum_gross,
        end.cum_fees,
        end.cum_gross.saturating_sub(end.cum_fees),
        resume.event_count.saturating_add(events.len())
    );
    eprintln!(
        "tick bars={} fills={} gross={} fees={}",
        candles.len(),
        t.fills,
        t.gross_micros,
        t.fees_micros
    );
    if let Some(rp) = &resume_path {
        store_resume(rp, &end);
    }
    if let Some(lp) = &ledger_path {
        append_ledger(lp, &events);
    }
    if let Some(sp) = &state_path
        && let Some(last) = candles.last()
    {
        let _ = std::fs::write(sp, last.open_ts_ms.to_string());
    }
}

/// Resume file: line-based, tolerant parse (bad lines warn + fall back).
/// ```text
/// v1
/// closed_realized <i64>
/// peak <i64 micros>
/// flat | position <LONG|SHORT> <entry_price_micros> <qty_base_micros> <entry_net>
/// window <open_ts_ms> <gross_micros> <fee_micros>   (repeated)
/// bar_offset <usize>
/// event_count <usize>
/// cum <fills> <gross> <fees>
/// ```
fn load_resume(path: &str) -> ResumeState {
    let text = match std::fs::read_to_string(path) {
        Ok(t) => t,
        Err(_) => return ResumeState::default(), // first tick: no file yet
    };
    let mut st = ResumeState::default();
    // v1 files predate bar/event/cum lines; missing lines mean 0 (fresh).
    for line in text.lines() {
        let f: Vec<&str> = line.split_whitespace().collect();
        match f.as_slice() {
            ["closed_realized", v] => {
                st.closed_realized = v.parse().unwrap_or(0);
            }
            ["peak", v] => {
                if let Ok(m) = v.parse() {
                    st.peak = Some(Money(m));
                }
            }
            ["flat"] => st.position = None,
            ["position", side, price, qty, net] => {
                let side = match *side {
                    "LONG" => Side::Long,
                    "SHORT" => Side::Short,
                    _ => {
                        eprintln!("resume: bad side {side:?}, ignoring position");
                        continue;
                    }
                };
                st.position = Some(ResumePosition {
                    side,
                    entry_price: Money(price.parse().unwrap_or(0)),
                    qty_base_micros: qty.parse().unwrap_or(0),
                    entry_net: net.parse().unwrap_or(0),
                });
            }
            ["window", ts, gross, fee] => {
                if let (Ok(ts), Ok(gross), Ok(fee)) = (ts.parse(), gross.parse(), fee.parse()) {
                    st.window_fills.push((ts, gross, fee));
                }
            }
            ["bar_offset", v] => {
                st.bar_offset = v.parse().unwrap_or(0);
            }
            ["event_count", v] => {
                st.event_count = v.parse().unwrap_or(0);
            }
            ["cum", fills, gross, fees] => {
                st.cum_fills = fills.parse().unwrap_or(0);
                st.cum_gross = gross.parse().unwrap_or(0);
                st.cum_fees = fees.parse().unwrap_or(0);
            }
            _ => {} // v1 header, blanks: skip
        }
    }
    st
}

fn store_resume(path: &str, end: &EndState) {
    let mut out = String::from("v1\n");
    out.push_str(&format!("closed_realized {}\n", end.closed_realized));
    out.push_str(&format!("peak {}\n", end.peak.0));
    match end.position {
        None => out.push_str("flat\n"),
        Some(p) => {
            let side = match p.side {
                Side::Long => "LONG",
                Side::Short => "SHORT",
            };
            out.push_str(&format!(
                "position {side} {} {} {}\n",
                p.entry_price.0, p.qty_base_micros, p.entry_net
            ));
        }
    }
    for (ts, gross, fee) in &end.window_fills {
        out.push_str(&format!("window {ts} {gross} {fee}\n"));
    }
    out.push_str(&format!("bar_offset {}\n", end.bar_offset));
    out.push_str(&format!("event_count {}\n", end.event_count));
    out.push_str(&format!(
        "cum {} {} {}\n",
        end.cum_fills, end.cum_gross, end.cum_fees
    ));
    if let Err(e) = std::fs::write(path, out) {
        eprintln!("resume write {path}: {e}");
    }
}

fn append_ledger(path: &str, events: &[nt_grid::PaperFillEvent]) {
    use std::fmt::Write as _;
    let fresh = !std::path::Path::new(path).exists();
    let mut out = String::new();
    if fresh {
        out.push_str("bar,side,reason,price_micros,qty_base_micros,fee_micros\n");
    }
    for e in events {
        let side = match e.side {
            Side::Long => "long",
            Side::Short => "short",
        };
        let reason = match e.reason {
            nt_grid::FillReason::Entry => "entry",
            nt_grid::FillReason::Target => "target",
            nt_grid::FillReason::Stop => "stop",
        };
        let _ = writeln!(
            out,
            "{},{side},{reason},{},{},{}",
            e.bar, e.price.0, e.qty_base_micros, e.fee.0
        );
    }
    if let Err(e) = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .and_then(|mut f| {
            use std::io::Write as _;
            f.write_all(out.as_bytes())
        })
    {
        eprintln!("ledger append {path}: {e}");
    }
}

/// Honest taker-exit default (6bp) without depending on nt-execution
/// (which would pull the RiskApproval seal into a paper-only binary).
fn nt_execution_bp() -> i64 {
    6
}
