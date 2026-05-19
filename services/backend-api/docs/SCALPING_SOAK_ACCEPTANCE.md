# Scalping Soak Acceptance Runbook

Use this runbook after the scalping fix stack is merged and deployed, or in a
paper/testnet/tightly capped live runtime that is meant to stand in for the
deployed system. The goal is to prove the current scalping behavior is no worse
than the broken live baseline that triggered the remediation.

## Broken Baseline

| Metric | Baseline |
| --- | --- |
| Bitget futures balance | About 48 USDT |
| Total trades | 68 total, 57 closed |
| Win rate | 12.3% |
| Total net PnL | -0.18 USDT |
| Total fees | -0.57 USDT |
| Avg PnL per trade | -0.003 USDT |
| Scalping cycles | 5,406 over about 24 days |
| Hold split | 74.5% |
| Regime split | 75.5% neutral, 19.1% trend |

## Preconditions

- Run against the merged/deployed build, not an unmerged PR worktree.
- Keep real execution disabled unless this is an explicitly approved tightly
  capped live soak.
- Use paper/testnet mode for normal acceptance.
- Confirm the runtime is healthy before starting:

```bash
bin/neuratrade gateway status
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/ready
```

## Acceptance Gates

The soak should pass these gates unless an operator records a deliberate waiver:

- `MIN_TRADES=20` when `REQUIRE_LIVE_TRIAL_READY=true`; otherwise `MIN_TRADES=1`
- `HOLD_PERIOD_SECONDS=300`
- `MIN_WIN_RATE=0.123`
- `MIN_NET_PNL=0`
- `MIN_AVG_NET_PNL=0`
- `MIN_SIGNAL_QUALITY_COVERAGE=1`
- `MAX_HOLD_RATIO=0.745`
- `MAX_DRAWDOWN_PCT=0.01`
- `MAX_AI_PROVIDER_DEGRADED_CYCLES=0`
- `MAX_PERFECT_WIN_TRADES=20`
- `MIN_BASELINE_WIN_RATE_DELTA=0`
- `MIN_BASELINE_NET_PNL_DELTA=0`
- `MIN_BASELINE_AVG_PNL_DELTA=0`
- `REQUIRE_LIVE_TRIAL_READY=false`

The artifact verifier enforces these same defaults.

`MAX_PERFECT_WIN_TRADES` is a paper-realism guard. If a paper soak closes more
than this many trades with 100% wins and zero drawdown, the evidence is treated
as insufficient even when PnL is positive. Use `MAX_PERFECT_WIN_TRADES=` only
for diagnostic runs where an operator explicitly accepts that perfect paper
results are not proof of production profitability.

`live_trial_readiness` is stricter than paper acceptance. It is only `ready=true`
when the paper sample has at least 20 closed trades, both wins and losses,
observed drawdown, complete signal-quality telemetry, no AI provider degradation,
acceptable hold ratio, and positive net/average PnL after fees. Set
`REQUIRE_LIVE_TRIAL_READY=true` only when a run is intended to approve a tightly
capped live/testnet trial; normal paper runs may be profitable but still not
ready for real-money signaling if the sample is too small or too clean.

For the real LLM no-order path, `services/backend-api/scripts/ai-scalping-probe.sh`
emits both synthetic proposal PnL and observed-exit paper PnL. Single-cycle
proposal exits remain `exit_observed=false`; observed paper trades close only
when a later market snapshot reaches SL/TP or the configured
`PAPER_HOLD_PERIOD_SECONDS` mark-to-market point. Treat short LLM probes as
provider/actionability evidence unless `paper_live_trial_readiness.ready=true`
is backed by `observed_paper_trades` reaching the live-trial minimum. Use
`REQUIRE_OBSERVED_LIVE_TRIAL_READY=true` when an LLM probe artifact is intended
to approve any real-money or tightly capped live/testnet signaling.

## Run

Use a timestamped artifact and database so the evidence can be retained:

```bash
services/backend-api/scripts/scalping-soak-acceptance.sh run
```

The acceptance wrapper performs runtime health preflight, writes timestamped
artifact and SQLite evidence paths, runs the artifact verifier, and emits a
small `.acceptance.json` manifest next to the soak artifact.

For manual runs, use the same defaults explicitly:

```bash
stamp="$(date +%Y%m%d%H%M%S)"
export SOAK_DB_PATH="${HOME}/.neuratrade/data/scalping-soak-acceptance-${stamp}.db"
export SOAK_OUTPUT_FILE="${HOME}/.neuratrade/data/scalping-soak-acceptance-${stamp}.json"
export CYCLES=60
export INTERVAL_MS=15000
export TIMEOUT_SECONDS=0
export HOLD_PERIOD_SECONDS=300
export CAPITAL=48
export MIN_TRADES=20
export MIN_WIN_RATE=0.123
export MIN_NET_PNL=0
export MIN_AVG_NET_PNL=0
export MIN_SIGNAL_QUALITY_COVERAGE=1
export MAX_HOLD_RATIO=0.745
export MAX_DRAWDOWN_PCT=0.01
export MAX_AI_PROVIDER_DEGRADED_CYCLES=0
export MAX_PERFECT_WIN_TRADES=20
export MIN_BASELINE_WIN_RATE_DELTA=0
export MIN_BASELINE_NET_PNL_DELTA=0
export MIN_BASELINE_AVG_PNL_DELTA=0
export REQUIRE_LIVE_TRIAL_READY=true

bash services/backend-api/scripts/scalping-soak.sh run
bash services/backend-api/scripts/verify-scalping-soak-artifact.sh "$SOAK_OUTPUT_FILE"
```

`TIMEOUT_SECONDS=0` lets the soak binary compute a budget from the requested
cycles and interval. If you need a fixed manual timeout for the defaults above,
use at least `3000` seconds so slow exchange calls do not expire before the
binary's computed budget.

`CYCLES=60`, `INTERVAL_MS=15000`, and `HOLD_PERIOD_SECONDS=300` give early
positions enough later observations to close honestly while still keeping a
single readiness run bounded. Short smoke soaks are useful for plumbing checks,
but they must not be used to signal real-money readiness.

## Evidence To Record

Record the verifier output and these artifact fields in the tracking issue:

```bash
jq -r '
  .result.report
  | {
      total_cycles,
      action_split,
      regime_split,
      rejection_by_reason,
      gate_block_by_code,
      signal_quality,
      trade_summary,
      ai_provider_degradation,
      baseline_comparison,
      insufficient_trade_proof,
      live_trial_readiness
    }
' "$SOAK_OUTPUT_FILE"
```

Also verify persistence if the verifier did not already check the DB path:

```bash
sqlite3 "$SOAK_DB_PATH" "
select 'scalping_cycle_telemetry', count(*) from scalping_cycle_telemetry
union all
select 'realized_pnl_journal', count(*) from realized_pnl_journal
union all
select 'positive_realized', count(*) from realized_pnl_journal where realized_pnl > 0
union all
select 'negative_realized', count(*) from realized_pnl_journal where realized_pnl < 0;
"
```

Before encoding a new deterministic entry rule, validate it against separate
observed soak DBs instead of a single winning window. For example:

```bash
python3 services/backend-api/scripts/validate-scalping-rule-candidate.py \
  --train-db /path/to/earlier-scalping-soak.db \
  --validation-db /path/to/latest-scalping-soak.db \
  --side buy \
  --min-imbalance 0.35 \
  --max-spread 0.06 \
  --max-range 35 \
  --min-recent 0.05 \
  --min-24h 0.02
```

Do not promote a rule that fails validation profitability or depends on a single
outlier trade after removing the best result.

## Pass Criteria

The run can close the final scalping acceptance item only when:

- The verifier exits successfully.
- The artifact reports `insufficient_trade_proof=false`.
- Persisted telemetry includes bid/ask spread, order book imbalance, range
  position, and 24h price change fields.
- The action split is no worse than the 74.5% hold baseline, or a documented
  market-regime waiver is filed with supporting evidence.
- Net PnL and average PnL per trade are non-negative after fees.
- AI provider degradation is zero.
- The result comes from a merged/deployed paper/testnet/tightly capped live
  runtime.
- `live_trial_readiness.ready=true` before any real-money or tiny live/testnet
  approval signal is sent.

If any criterion fails, keep the acceptance issue open and file a concrete
follow-up with the failed metric, artifact path, database path, and command used.
