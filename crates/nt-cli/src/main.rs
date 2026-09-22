// ponytail: shadow runner — replays closed-candle CSV through the native
// paper engine and prints ledger totals. Paper-only: no venue, no orders,
// no secrets. Input CSV columns: open_ts_ms,open,high,low,close micros.
// (Export with: sqlite3 demo.db ".headers on" "SELECT ..." > bars.csv)
// Live-shadow runs: pass --capital-usdt = per-symbol partition. Production
// default is 10 USDT per symbol (min_capital floor 1, so the floor never
// itself closes trading). + per-symbol --fee-bp, else parity diffs are
// config artifacts, not engine divergence. Fees follow the champion soak's
// honestFees schedule: --fee-bp is TAKER (entries + stop exits, 6bp) and
// --maker-fee-bp is MAKER (resting target exits, 2bp), each defaulting
// independently. The two flags are independent, so a per-symbol drag run
// (--fee-bp 69) charges target exits at the 2bp DEFAULT unless
// --maker-fee-bp 69 is passed too; pass BOTH to reproduce the old flat
// model on every fill.
// --state is a tick cursor (last open_ts): each tick replays only newer
// candles instead of double-counting. --resume persists open position +
// equity state so an exit whose entry sat in a prior tick still fires;
// without it a fresh engine replays a partial window and drops those exits.
// --ledger appends this tick's fills as CSV. All three are opt-in file
// paths (default off); no network, no secrets.
// Geometry flags (--step-bp/--target-x100/--stop-grids/--slip-bp/--pos-pct)
// default to the paper_engine_check fixture scale; pass champion-soak.json
// knobs (step130/target195/stop2) for champion parity. Untouched defaults
// replay byte-identical.
use nt_grid::{
    EndState, PaperEngineConfig, ResumePosition, ResumeState, Side, run_paper_engine_from,
};
use nt_market::Candle;
use nt_risk::{Money, RiskLimits};
use std::env;

fn usage() -> ! {
    eprintln!(
        "usage: nt-cli shadow --bars <csv> [--capital-usdt N] [--fee-bp N] [--maker-fee-bp N] [--timeframe-ms N] [--state <file>] [--resume <file>] [--ledger <file>] [--step-bp N] [--target-x100 N] [--stop-grids N] [--slip-bp N] [--pos-pct N]"
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
        Some("ladder-shadow") => ladder_shadow(&args),
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
    let mut capital_usdt: i64 = 10;
    let mut fee_bp: i64 = nt_execution_bp();
    let mut maker_fee_bp: i64 = nt_execution_maker_bp();
    let mut timeframe_ms: Option<i64> = None;
    let mut state_path: Option<String> = None;
    let mut resume_path: Option<String> = None;
    let mut ledger_path: Option<String> = None;
    let mut step_bp: i64 = 100;
    let mut target_x100: i64 = 100;
    let mut stop_grids: i64 = 2;
    let mut slip_bp: i64 = 50;
    let mut pos_pct: i64 = 10;
    let mut fetch_url: Option<String> = None;
    let mut fetch_out: Option<String> = None;
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
            "--maker-fee-bp" => {
                i += 1;
                maker_fee_bp = args
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
            "--step-bp" => {
                i += 1;
                step_bp = args
                    .get(i)
                    .unwrap_or_else(|| usage())
                    .parse()
                    .unwrap_or_else(|_| usage());
            }
            "--target-x100" => {
                i += 1;
                target_x100 = args
                    .get(i)
                    .unwrap_or_else(|| usage())
                    .parse()
                    .unwrap_or_else(|_| usage());
            }
            "--stop-grids" => {
                i += 1;
                stop_grids = args
                    .get(i)
                    .unwrap_or_else(|| usage())
                    .parse()
                    .unwrap_or_else(|_| usage());
            }
            "--slip-bp" => {
                i += 1;
                slip_bp = args
                    .get(i)
                    .unwrap_or_else(|| usage())
                    .parse()
                    .unwrap_or_else(|_| usage());
            }
            "--pos-pct" => {
                i += 1;
                pos_pct = args
                    .get(i)
                    .unwrap_or_else(|| usage())
                    .parse()
                    .unwrap_or_else(|_| usage());
            }
            "--fetch-public" => {
                i += 1;
                fetch_url = Some(args.get(i).unwrap_or_else(|| usage()).clone());
            }
            "--fetch-out" => {
                i += 1;
                fetch_out = Some(args.get(i).unwrap_or_else(|| usage()).clone());
            }
            _ => usage(),
        }
        i += 1;
    }
    // Public-candle fetch runs BEFORE --bars is required: it PRODUCES the
    // CSV (`--fetch-public <url> --fetch-out <csv>` + nothing else).
    if let (Some(url), Some(out)) = (&fetch_url, &fetch_out) {
        if let Err(e) = fetch_public_candles(url, out) {
            eprintln!("fetch {url}: {e}");
            std::process::exit(1);
        }
        println!("fetched {url} -> {out}");
        return;
    } else if fetch_url.is_some() || fetch_out.is_some() {
        eprintln!("--fetch-public and --fetch-out must be passed together");
        std::process::exit(2);
    }
    let path = bars.unwrap_or_else(|| usage());
    let mut candles = load_panel(&path, timeframe_ms);
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
    // Paper-engine geometry: fixture scale by default, champion knobs via
    // flags (step130/target195/stop2 for champion-soak.json parity).
    // Fee per symbol via --fee-bp (taker: entries + stop exits) and
    // --maker-fee-bp (resting target exits), mirroring the soak's
    // honestFees maker0.02/takerExit0.06 schedule.
    let cfg = PaperEngineConfig {
        step_bp,
        target_ratio_x100: target_x100,
        grid_max_grids: stop_grids,
        slippage_bps: slip_bp,
        max_position_size_pct: pos_pct,
        fee_bp,
        maker_fee_bp,
    };
    let capital = Money(capital_usdt * 1_000_000);
    // Production runs 10 USDT per symbol. The floor must sit BELOW that or
    // every entry is rejected — and it must not be derived from capital
    // itself, or a small drawdown would close trading permanently.
    let min_capital = Money(1_000_000);
    let limits = RiskLimits {
        min_capital,
        // Champion sizing parity: the entry sizes position_value at pos_pct
        // of capital, so the risk cap must match or every entry is rejected.
        max_position_size_pct: pos_pct,
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

/// Panel load, shared by `shadow` and `ladder-shadow`: parse 5-col micros
/// CSV, oldest-first sort, then the nt-market closed-only contract — with
/// `--timeframe-ms` a bar is closed iff now >= open_ts + timeframe; without
/// it, drop the newest row (a CSV export's tail is the forming candle).
/// Never both.
fn load_panel(path: &str, timeframe_ms: Option<i64>) -> Vec<Candle> {
    let text = std::fs::read_to_string(path).unwrap_or_else(|e| {
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
    candles.sort_by_key(|c| c.open_ts_ms); // oldest-first contract for the engines
    if let Some(tf) = timeframe_ms {
        let now_ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_millis() as i64)
            .unwrap_or(i64::MAX);
        candles.retain(|c| c.open_ts_ms.saturating_add(tf) <= now_ms);
    } else if candles.len() > 1 {
        candles.pop();
    }
    candles
}

/// `nt-cli ladder-shadow`: replay a closed-candle panel through the native
/// LADDER engine (`nt-ladder`) — the Gate 4 ladder-half primitive. Paper
/// only: no venue, no orders, no secrets, same CSV contract as `shadow`.
///
/// Tick continuity lives entirely in `--resume` (ladder v3): the state blob
/// carries BOTH the engine state and the candle cursor (`last_ts`), so
/// there is no separate `--state` flag — two files would drift apart.
/// Fresh (no resume): starts at window index 1, mirroring TS
/// `resolveLadderStartIndex` (`ladder-engine.ts:1733-1740`, fresh +
/// non-forwardOnly + no trend => `Math.max(1, trendFilterPeriod)` = 1):
/// bar 0 is the seed/prev reference and is NEVER processed.
/// `bar_index` is always the index into THIS window — the coordinate
/// `Rung::entry_bar` and the conservative same-bar gate both use.
///
/// Defaults are the deployed champion soak's ladder knobs (read from
/// `ecosystem.champion-soak.config.cjs` args + `knobs.*` + PM2 process
/// args): step 1.3%, target 1.95, stopRatio 1.58, pause 2, grids 2,
/// maxHold 39, fee 0.02 both sides, cross 15bp, slip 2bp, pos 50%,
/// drawdown 15%, 2 rungs, 15m, 50 USDT per symbol (200/4). Pass the
/// matching flags explicitly on any diff artifact so the basis is in the
/// command line, not in this comment.
fn ladder_shadow(args: &[String]) -> ! {
    fn usage_ladder() -> ! {
        eprintln!(
            "usage: nt-cli ladder-shadow --bars <csv> [--capital-usdt N] [--timeframe-ms N] \
             [--resume <file>] [--ledger <file>] [--rungs N] [--step-bp N] [--target-x100 N] \
             [--stop-x100 N] [--max-grids N] [--pause-bars N] [--max-hold-bars N] \
             [--max-drawdown-pct N] [--conservative 0|1] [--fee-bp N] [--maker-fee-bp N] \
             [--cross-bp N] [--slip-bp N] [--pos-pct N] [--leverage N]"
        );
        std::process::exit(2);
    }
    let mut bars: Option<String> = None;
    let mut capital_usdt: i64 = 50; // soak: 200 / 4 symbols
    let mut timeframe_ms: i64 = 900_000; // 15m
    let mut resume_path: Option<String> = None;
    let mut ledger_path: Option<String> = None;
    let mut rungs: u32 = 2;
    let mut step_bp: i64 = 130; // knobs.gridStepPct 1.3
    let mut target_x100: i64 = 195; // knobs.targetRatio 1.95
    let mut stop_x100: i64 = 158; // knobs.stopRatio 1.58
    let mut max_grids: i64 = 2; // knobs.gridMaxGrids 2
    let mut pause_bars: i64 = 2; // knobs.gridPauseAfterLossBars 2
    let mut max_hold_bars: i64 = 39; // knobs.maxHoldBars 39
    let mut max_drawdown_pct: i64 = 15; // --max-drawdown-pct 15
    let mut conservative: i64 = 1; // TS option default true
    let mut fee_bp: i64 = 2; // --fee 0.02 (taker falls back to maker)
    let mut maker_fee_bp: i64 = 2;
    let mut cross_bp: i64 = 15; // --live-entry-cross-bps 15
    let mut slip_bp: i64 = 2; // --slippage-bps 2
    let mut pos_pct: i64 = 50; // --max-position-size-pct 50
    let mut leverage: i64 = 1; // --leverage 1
    let mut i = 2;
    let mut next_i64 = |i: &mut usize| -> i64 {
        *i += 1;
        args.get(*i)
            .unwrap_or_else(|| usage_ladder())
            .parse()
            .unwrap_or_else(|_| usage_ladder())
    };
    let _ = &mut next_i64; // closure used below via macro-free helper
    let arg = |i: &mut usize| -> String {
        *i += 1;
        args.get(*i).unwrap_or_else(|| usage_ladder()).clone()
    };
    while i < args.len() {
        match args[i].as_str() {
            "--bars" => bars = Some(arg(&mut i)),
            "--resume" => resume_path = Some(arg(&mut i)),
            "--ledger" => ledger_path = Some(arg(&mut i)),
            "--capital-usdt" => capital_usdt = next_i64(&mut i),
            "--timeframe-ms" => timeframe_ms = next_i64(&mut i),
            "--rungs" => rungs = next_i64(&mut i).max(1) as u32,
            "--step-bp" => step_bp = next_i64(&mut i),
            "--target-x100" => target_x100 = next_i64(&mut i),
            "--stop-x100" => stop_x100 = next_i64(&mut i),
            "--max-grids" => max_grids = next_i64(&mut i),
            "--pause-bars" => pause_bars = next_i64(&mut i),
            "--max-hold-bars" => max_hold_bars = next_i64(&mut i),
            "--max-drawdown-pct" => max_drawdown_pct = next_i64(&mut i),
            "--conservative" => conservative = next_i64(&mut i),
            "--fee-bp" => fee_bp = next_i64(&mut i),
            "--maker-fee-bp" => maker_fee_bp = next_i64(&mut i),
            "--cross-bp" => cross_bp = next_i64(&mut i),
            "--slip-bp" => slip_bp = next_i64(&mut i),
            "--pos-pct" => pos_pct = next_i64(&mut i),
            "--leverage" => leverage = next_i64(&mut i),
            _ => usage_ladder(),
        }
        i += 1;
    }
    let path = bars.unwrap_or_else(|| usage_ladder());
    let candles = load_panel(&path, Some(timeframe_ms));

    let cfg = nt_ladder::LadderConfig {
        grid_step_bp: step_bp,
        grid_max_grids: max_grids,
        grid_pause_after_loss_bars: pause_bars,
        rungs,
        target_ratio_x100: target_x100,
        only_with_trend: false, // soak trend-filter-period 0; no trend series here
        chop_gate_adx: 0,       // soak chop-gate-adx 0 (fingerprint field)
        max_hold_bars,
        stop_ratio_x100: stop_x100,
        conservative_intrabar: conservative != 0,
    };
    let sizing = nt_ladder::SizingOptions {
        max_position_pct: pos_pct,
        max_notional_pct: None, // soak sets no notional cap
        rungs: i64::from(rungs),
        leverage,
        fully_dynamic: false, // soak runs fixed --leverage 1
        max_leverage: 10,     // TS default
        spec: None,           // no contractSpecs in the replay path
        maker_fee_bp,
        taker_exit_fee_bp: Some(fee_bp), // independent of maker, like grid --fee-bp
        live_entry_cross_bps: cross_bp,
    };

    // Resume doubles as the cursor: missing file => fresh (TS startIndex 1).
    let mut state = match &resume_path {
        None => nt_ladder::LadderState::fresh(Money(capital_usdt * 1_000_000), cfg),
        Some(rp) => match std::fs::read_to_string(rp) {
            Err(_) => nt_ladder::LadderState::fresh(Money(capital_usdt * 1_000_000), cfg),
            Ok(text) => match nt_ladder::load(&text) {
                Ok(st) => st,
                Err(e) => {
                    eprintln!("resume {rp}: {e}");
                    std::process::exit(1);
                }
            },
        },
    };
    // TS `resolveLadderStartIndex`: fresh => 1 (bar 0 never processed);
    // resumed => first candle newer than lastTimestamp, else hold.
    let start = match state.last_ts_ms {
        None => 1.min(candles.len()),
        Some(ts) => candles
            .iter()
            .position(|c| c.open_ts_ms > ts)
            .unwrap_or(candles.len()),
    };

    let mut fills = 0usize;
    let mut closes = 0usize;
    let mut target = 0usize;
    let mut stop = 0usize;
    let mut maxhold = 0usize;
    let mut liquidation = 0usize;
    for (k, c) in candles[start..].iter().enumerate() {
        let bar_index = (start + k) as u64; // window coordinate (entry_bar)
        // BarContext::step = open * gridStepPct / 100 (TS
        // createLadderBarContext), re-derived EVERY bar.
        let step = Money((i128::from(c.open.0) * i128::from(step_bp) / 10_000) as i64);
        let ctx = nt_ladder::BarContext {
            bar_index,
            candle: nt_ladder::Candle {
                open: c.open,
                high: c.high,
                low: c.low,
                close: c.close,
                ts_ms: c.open_ts_ms,
            },
            step,
            slippage_bp: slip_bp,
            rung_count: rungs,
            target_ratio_x100: target_x100,
            max_hold_bars,
            ms_per_bar: timeframe_ms,
            conservative_intrabar: conservative != 0,
            max_drawdown_pct,
        };
        let ev = nt_ladder::advance_bar(&mut state, &ctx, sizing);
        fills += ev.fills.len();
        closes += ev.closes.len();
        for c in &ev.closes {
            match c.reason {
                nt_ladder::CloseReason::Target => target += 1,
                nt_ladder::CloseReason::Stop => stop += 1,
                nt_ladder::CloseReason::MaxHold => maxhold += 1,
                nt_ladder::CloseReason::Liquidation => liquidation += 1,
            }
        }
        if let Some(lp) = &ledger_path {
            append_ladder_ledger(lp, &ev.closes);
        }
    }
    let open = state
        .rungs(nt_ladder::Side::Long)
        .iter()
        .filter(|r| r.filled)
        .count()
        + state
            .rungs(nt_ladder::Side::Short)
            .iter()
            .filter(|r| r.filled)
            .count();
    // Machine-readable line: the daily Gate 4 diff parses this next to the
    // TS ledger. Reasons are itemised so a max_hold timeout can never be
    // counted as target/stop edge (Gate 3 lesson).
    println!(
        "ladder-shadow bars={} fills={} closes={} target={} stop={} maxhold={} liquidation={} \
         wins={} losses={} capital={} paused={} open={}",
        state.bars_consumed,
        fills,
        closes,
        target,
        stop,
        maxhold,
        liquidation,
        state.total_wins,
        state.total_losses,
        state.capital.0,
        state.paused,
        open,
    );
    eprintln!(
        "tick bars={} fills={} closes={}",
        candles.len().saturating_sub(start),
        fills,
        closes
    );
    if let Some(rp) = &resume_path
        && let Err(e) = std::fs::write(rp, nt_ladder::save(&state))
    {
        eprintln!("resume write {rp}: {e}");
    }
    std::process::exit(0);
}

/// Append this tick's ladder closes to a CSV ledger (created with a header
/// on first use): one row per closed rung, reasons itemised.
fn append_ladder_ledger(path: &str, closes: &[nt_ladder::CloseEvent]) {
    use std::fmt::Write as _;
    use std::io::Write as _;
    let fresh = !std::path::Path::new(path).exists();
    let mut out = String::new();
    if fresh {
        out.push_str(
            "closed_ts_ms,rung_index,side,reason,entry_micros,exit_micros,pnl_micros,\
             qty_base_micros,capital_after_micros\n",
        );
    }
    for c in closes {
        let _ = writeln!(
            out,
            "{},{},{:?},{:?},{},{},{},{},{}",
            c.closed_ts_ms,
            c.rung_index,
            c.side,
            c.reason,
            c.entry_price.0,
            c.exit_price.0,
            c.pnl.0,
            c.qty,
            c.capital_after.0
        );
    }
    if let Err(e) = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .and_then(|mut f| f.write_all(out.as_bytes()))
    {
        eprintln!("ledger append {path}: {e}");
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
            // v2 day boundary (i64::MIN = unset). v1 files lack these lines,
            // so an old resume reads as "no boundary" and re-establishes.
            ["day_index", v] => match v.parse::<i64>() {
                Ok(d) if d != i64::MIN => st.day_index = Some(d),
                _ => st.day_index = None,
            },
            ["day_fills", v] => {
                st.day_fills = v.parse().unwrap_or(0);
            }
            ["day_start", v] => {
                if let Ok(m) = v.parse::<i64>() {
                    st.day_start_capital = Some(Money(m));
                }
            }
            _ => {} // v1 header, blanks: skip
        }
    }
    st
}

fn store_resume(path: &str, end: &EndState) {
    let mut out = String::from("v2\n");
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
    out.push_str(&format!(
        "day_index {}\n",
        end.day_index.unwrap_or(i64::MIN)
    ));
    out.push_str(&format!("day_fills {}\n", end.day_fills));
    out.push_str(&format!("day_start {}\n", end.day_start_capital.0));
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

/// Public-candle fetch: plain unauthenticated HTTP GET (no auth headers,
/// no signing) of a JSON array of [open_ts_ms, open, high, low, close]
/// integer-micros rows, written as --bars-compatible CSV. Uses only std
/// (no new deps): minimal HTTP/1.0 over TcpStream, http:// URLs only —
/// TLS stays in the TS soak until a vendored TLS crate is approved.
/// Fail-closed: non-200, short read, or bad JSON aborts with an error.
fn fetch_public_candles(url: &str, out: &str) -> Result<(), String> {
    use std::io::{Read, Write};
    let rest = url
        .strip_prefix("http://")
        .ok_or("only http:// URLs (no TLS yet)")?;
    let (host, path) = match rest.find('/') {
        Some(i) => (&rest[..i], &rest[i..]),
        None => (rest, "/"),
    };
    let mut sock = std::net::TcpStream::connect((host, 80)).map_err(|e| e.to_string())?;
    sock.set_read_timeout(Some(std::time::Duration::from_secs(20)))
        .map_err(|e| e.to_string())?;
    write!(
        sock,
        "GET {path} HTTP/1.0\r\nHost: {host}\r\nConnection: close\r\n\r\n"
    )
    .map_err(|e| e.to_string())?;
    let mut raw = Vec::new();
    sock.read_to_end(&mut raw).map_err(|e| e.to_string())?;
    let text = String::from_utf8_lossy(&raw);
    let (_, body) = text.split_once("\r\n\r\n").ok_or("no HTTP body")?;
    let status = text.lines().next().unwrap_or("");
    if !status.contains(" 200") {
        return Err(format!("status: {status}"));
    }
    // Minimal JSON parse: expect [[n,n,n,n,n],...] with integer micros.
    let nums: Vec<i64> = body
        .split(|c: char| !(c == '-' || c.is_ascii_digit()))
        .filter(|s| !s.is_empty())
        .map(|s| s.parse().map_err(|_| format!("bad number {s:?}")))
        .collect::<Result<_, _>>()?;
    if nums.is_empty() || !nums.len().is_multiple_of(5) {
        return Err(format!("want 5-col rows, got {} numbers", nums.len()));
    }
    let mut csv = String::from("open_ts_ms,open,high,low,close\n");
    let (rows, _) = nums.as_chunks::<5>();
    for r in rows {
        use std::fmt::Write as _;
        let _ = writeln!(csv, "{},{},{},{},{}", r[0], r[1], r[2], r[3], r[4]);
    }
    std::fs::write(out, csv).map_err(|e| e.to_string())?;
    Ok(())
}

/// Honest taker-exit default (6bp) without depending on nt-execution
/// (which would pull the RiskApproval seal into a paper-only binary).
/// Mirrors `nt_execution::HONEST_TAKER_EXIT_BP`; the maker helper mirrors
/// `nt_execution::HONEST_MAKER_FEE_BP` (2bp) for the same reason — the
/// champion soak's `honestFees` schedule maker0.02/takerExit0.06. Keep the
/// two literals in sync if the soak schedule is ever rescored.
fn nt_execution_bp() -> i64 {
    6
}

fn nt_execution_maker_bp() -> i64 {
    2
}
