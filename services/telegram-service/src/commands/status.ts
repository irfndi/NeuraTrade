import type {
  AIStatusResponse,
  AiRuntimeState,
  ChatRuntimeState,
  DoctorCheckResponse,
  DoctorResponse,
  GetUserByChatIdResponse,
  LogsResponse,
  NotificationPreferenceResponse,
  PortfolioResponse,
  QuestDiagnosticsResponse,
  QuestRuntimeState,
  TopCandidateRejection,
  TradingModeResponse,
} from "../api/types";
import type { ClassifiableError } from "../../telegram-errors";
import { logger } from "../utils/logger";

function normalizeStatus(status: string): "healthy" | "warning" | "critical" {
  const lowered = status.toLowerCase();
  if (
    lowered === "healthy" ||
    lowered === "warning" ||
    lowered === "critical"
  ) {
    return lowered;
  }
  return "critical";
}

function iconForStatus(status: "healthy" | "warning" | "critical"): string {
  switch (status) {
    case "healthy":
      return "✅";
    case "warning":
      return "⚠️";
    default:
      return "❌";
  }
}

function summarizeCheck(
  checks: readonly DoctorCheckResponse[] | undefined,
  checkName: string,
): string {
  if (!checks || checks.length === 0) {
    return "unknown";
  }
  const found = checks.find((check) => check.name === checkName);
  if (!found) {
    return "unknown";
  }
  const status = normalizeStatus(found.status);
  const detail = found.message ? ` (${found.message})` : "";
  return `${iconForStatus(status)} ${status.toUpperCase()}${detail}`;
}

function summarizeMode(mode: TradingModeResponse): string {
  const modeLabel = mode.mode?.toUpperCase() || "DRY";
  return mode.mode === "live"
    ? modeLabel
    : `${modeLabel} (${mode.confirmations}/${mode.required_confirmations} confirmations)`;
}

function summarizeAI(ai: AIStatusResponse): string {
  const model = ai.selected_model || "none selected";
  const provider = ai.provider || "n/a";
  const budget = ai.daily_budget_exceeded ? "budget exceeded" : "budget ok";
  const readiness = ai.readiness || "unavailable";

  if (readiness === "ready_auto_route") {
    const effectiveProvider = ai.effective_provider || provider;
    const effectiveModel = ai.effective_model || "auto";
    const chainInfo = ai.provider_chain_usable
      ? `chain ${ai.provider_chain_usable} usable`
      : "";
    return `auto-route → ${effectiveModel} (${effectiveProvider}${chainInfo ? ", " + chainInfo : ""}, ${budget})`;
  }
  if (readiness === "ready") {
    return `${model} (${provider}, ${budget})`;
  }
  if (readiness === "degraded") {
    const chainInfo = ai.provider_chain_configured
      ? `chain ${ai.provider_chain_usable || 0}/${ai.provider_chain_configured} usable`
      : "degraded";
    return `${model} (${provider}, ${chainInfo}, ${budget})`;
  }
  if (readiness === "unavailable") {
    return `unavailable (n/a, ${budget})`;
  }
  return `none selected (n/a, ${budget})`;
}

function shortError(error: ClassifiableError): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

/** Which hard gate currently blocks new entries, evaluated in priority order. */
type EntryGatePriority =
  | "risk_lock"
  | "state_drift"
  | "runtime_circuit"
  | "rollout_gate"
  | "recovery_gate"
  | "none";

/** Entry-gate fields resolved from chat runtime state with diagnostics fallbacks. */
interface GateSnapshot {
  entryGateReason: string | undefined;
  entryGateType: string;
  riskLockSource: string;
  runtimeCircuitActive: boolean;
  entryAttemptBlockReason: string | undefined;
  nextUnblockCondition: string | undefined;
}

/** Rollout + progress-watchdog fields resolved from chat runtime state. */
interface RolloutSnapshot {
  stage: string | undefined;
  status: string | undefined;
  gateReason: string | undefined;
  progressBlocked: boolean;
  progressBlockReason: string | undefined;
}

/** Candidate funnel counters and top rejections. */
interface CandidateSnapshot {
  universe: number | undefined;
  ranked: number | undefined;
  viable: number | undefined;
  rejections: readonly TopCandidateRejection[];
}

/** Entry-attempt / drift-cycle fields resolved from chat runtime state. */
interface AttemptSnapshot {
  entryAttempts1h: number | undefined;
  lastEntryAttemptAt: string | undefined;
  minutesSinceEntryAttempt: number | undefined;
  driftDeadlockCycles: number | undefined;
  driftSignature: string | undefined;
}

/** Execution loop fields resolved from chat runtime with quest runtime fallbacks. */
interface ExecutionSnapshot {
  stage: string | undefined;
  lastProgressAt: string | undefined;
  inProgressAgeSeconds: number | undefined;
}

/** Effective trading thresholds resolved from chat runtime state. */
interface ThresholdSnapshot {
  accountTier: string | undefined;
  effectiveMinConfidence: number | undefined;
  effectiveMaxCapitalPct: number | undefined;
  effectiveMaxConcurrentPositions: number | undefined;
  managedOpenPositionsEffective: number | undefined;
}

/** One rejected backend probe paired with its log label. */
interface RejectedResultEntry {
  label: string;
  result: PromiseSettledResult<unknown>;
}

/** Minimal grammY context the /status handler reads and replies through. */
export interface StatusCommandContext {
  chat?: { id?: number | string };
  from?: { id?: number | string };
  reply(text: string): Promise<unknown>;
}

/** Minimal bot surface the /status handler depends on. */
export interface StatusCommandBot {
  command(
    name: string,
    handler: (ctx: StatusCommandContext) => Promise<void> | void,
  ): void;
}

/** Minimal backend API surface the /status handler depends on. */
export interface StatusCommandApi {
  getUserByChatId(chatId: string): Promise<GetUserByChatIdResponse | null>;
  getNotificationPreference(
    userId: string,
  ): Promise<NotificationPreferenceResponse>;
  getDoctor(chatId: string): Promise<DoctorResponse>;
  getTradingMode(chatId: string): Promise<TradingModeResponse>;
  getPortfolio(chatId: string): Promise<PortfolioResponse>;
  getAIStatus(chatId: string): Promise<AIStatusResponse>;
  getLogs(chatId: string, limit?: number): Promise<LogsResponse>;
  getQuestDiagnostics(chatId: string): Promise<QuestDiagnosticsResponse>;
}

function gateSnapshot(
  diagnostics: QuestDiagnosticsResponse,
  questRuntime: QuestRuntimeState | undefined,
  chatRuntime: ChatRuntimeState | undefined,
  aiRuntime: AiRuntimeState | undefined,
): GateSnapshot {
  return {
    entryGateReason:
      chatRuntime?.entry_gate_reason_current ||
      diagnostics.entry_gate_reason_current,
    entryGateType: chatRuntime?.entry_gate_type || "none",
    riskLockSource:
      chatRuntime?.risk_lock_source ||
      questRuntime?.risk_lock_source ||
      diagnostics.risk_lock_source ||
      "none",
    runtimeCircuitActive: aiRuntime?.circuit_active === true,
    entryAttemptBlockReason:
      chatRuntime?.entry_attempt_block_reason ||
      diagnostics.entry_attempt_block_reason,
    nextUnblockCondition:
      chatRuntime?.next_unblock_condition_current ||
      diagnostics.next_unblock_condition_current,
  };
}

function rolloutSnapshot(
  diagnostics: QuestDiagnosticsResponse,
  chatRuntime: ChatRuntimeState | undefined,
): RolloutSnapshot {
  return {
    stage: chatRuntime?.rollout_stage_current || diagnostics.rollout_stage_current,
    status:
      chatRuntime?.rollout_status_current || diagnostics.rollout_status_current,
    gateReason:
      chatRuntime?.rollout_gate_reason_current ||
      diagnostics.rollout_gate_reason_current,
    progressBlocked:
      chatRuntime?.progress_blocked ?? diagnostics.progress_blocked ?? false,
    progressBlockReason:
      chatRuntime?.progress_block_reason || diagnostics.progress_block_reason,
  };
}

function candidateSnapshot(
  diagnostics: QuestDiagnosticsResponse,
  chatRuntime: ChatRuntimeState | undefined,
): CandidateSnapshot {
  const chatRejections = chatRuntime?.top_candidate_rejections;
  return {
    universe:
      chatRuntime?.candidate_universe_count ??
      diagnostics.candidate_universe_count,
    ranked:
      chatRuntime?.candidate_ranked_count ?? diagnostics.candidate_ranked_count,
    viable:
      chatRuntime?.candidate_viable_count ?? diagnostics.candidate_viable_count,
    rejections:
      chatRejections && chatRejections.length > 0
        ? chatRejections
        : (diagnostics.top_candidate_rejections ?? []),
  };
}

function attemptSnapshot(
  diagnostics: QuestDiagnosticsResponse,
  chatRuntime: ChatRuntimeState | undefined,
): AttemptSnapshot {
  return {
    entryAttempts1h:
      chatRuntime?.entry_attempts_1h ?? diagnostics.entry_attempts_1h,
    lastEntryAttemptAt:
      chatRuntime?.last_entry_attempt_at || diagnostics.last_entry_attempt_at,
    minutesSinceEntryAttempt:
      chatRuntime?.minutes_since_entry_attempt ??
      diagnostics.minutes_since_entry_attempt,
    driftDeadlockCycles:
      chatRuntime?.drift_deadlock_cycles ?? diagnostics.drift_deadlock_cycles,
    driftSignature: chatRuntime?.drift_signature || diagnostics.drift_signature,
  };
}

function executionSnapshot(
  diagnostics: QuestDiagnosticsResponse,
  questRuntime: QuestRuntimeState | undefined,
  chatRuntime: ChatRuntimeState | undefined,
): ExecutionSnapshot {
  return {
    stage: chatRuntime?.execution_stage || questRuntime?.execution_stage,
    lastProgressAt:
      chatRuntime?.execution_last_progress_at ||
      questRuntime?.execution_last_progress_at,
    inProgressAgeSeconds:
      chatRuntime?.execution_in_progress_age_seconds ??
      questRuntime?.execution_in_progress_age_seconds,
  };
}

function thresholdSnapshot(
  diagnostics: QuestDiagnosticsResponse,
  chatRuntime: ChatRuntimeState | undefined,
): ThresholdSnapshot {
  return {
    accountTier: chatRuntime?.account_tier || diagnostics.account_tier,
    effectiveMinConfidence:
      chatRuntime?.effective_min_confidence ??
      diagnostics.effective_min_confidence,
    effectiveMaxCapitalPct:
      chatRuntime?.effective_max_capital_pct ??
      diagnostics.effective_max_capital_pct,
    effectiveMaxConcurrentPositions:
      chatRuntime?.effective_max_concurrent_positions ??
      diagnostics.effective_max_concurrent_positions,
    managedOpenPositionsEffective:
      chatRuntime?.managed_open_positions_effective ??
      diagnostics.managed_open_positions_effective,
  };
}

function entryGatePriorityFor(
  riskLock: boolean | undefined,
  driftActive: boolean,
  entryGateType: string,
  runtimeCircuitActive: boolean,
  rolloutGateReasonCurrent: string | undefined,
): EntryGatePriority {
  if (riskLock === true || entryGateType === "risk_lock") {
    return "risk_lock";
  }
  if (driftActive || entryGateType === "state_drift") {
    return "state_drift";
  }
  if (runtimeCircuitActive || entryGateType === "runtime_circuit") {
    return "runtime_circuit";
  }
  if (
    entryGateType === "rollout_gate" ||
    (entryGateType === "none" && rolloutGateReasonCurrent)
  ) {
    return "rollout_gate";
  }
  if (entryGateType === "recovery_gate") {
    return "recovery_gate";
  }
  return "none";
}

function blockerReasonFor(
  priority: EntryGatePriority,
  driftCount: number | undefined,
  gate: GateSnapshot,
  rollout: RolloutSnapshot,
): string {
  switch (priority) {
    case "risk_lock":
      return gate.riskLockSource === "manual_env"
        ? "forced by operator env lock"
        : "global risk lock active";
    case "state_drift":
      return driftCount !== undefined
        ? `lifecycle drift pending reconcile (${driftCount})`
        : "lifecycle drift pending reconcile";
    case "runtime_circuit":
      return "AI runtime circuit is open";
    case "rollout_gate":
      return (
        rollout.gateReason ||
        "strategy is not live yet; rollout still blocks execution"
      );
    case "recovery_gate":
      return "recovery clean-cycle gate active";
    default:
      return "";
  }
}

function pushEntryGateLines(
  lines: string[],
  gate: GateSnapshot,
  rollout: RolloutSnapshot,
  priority: EntryGatePriority,
  driftCount: number | undefined,
): void {
  if (gate.entryGateReason) {
    lines.push(`• Entry gate reason: ${gate.entryGateReason}`);
  }
  // blockerReason is derived ONLY from hard gate priority, not candidate-level rejections.
  // Candidate rejections (entryAttemptBlockReason) are shown separately below.
  const blockerReason =
    gate.entryGateReason || blockerReasonFor(priority, driftCount, gate, rollout);
  // When no hard gate is active, show just "none" without confusing parenthetical.
  // Candidate-level rejections are displayed in the separate "Entry attempt block" line.
  let blockerDisplay = "none";
  if (priority !== "none") {
    blockerDisplay = `${priority}${blockerReason ? ` (${blockerReason})` : ""}`;
  }
  lines.push(`• Entry blocker: ${blockerDisplay}`);
  if (rollout.stage || rollout.status) {
    lines.push(
      `• Rollout: ${rollout.stage || "unknown"}/${rollout.status || "unknown"}`,
    );
  }
  if (rollout.gateReason) {
    lines.push(`• Rollout gate: ${rollout.gateReason}`);
  }
  if (priority === "risk_lock") {
    lines.push(`• Risk lock source: ${gate.riskLockSource}`);
  }
  if (gate.entryAttemptBlockReason) {
    lines.push(`• Entry attempt block: ${gate.entryAttemptBlockReason}`);
  }
  const resolvedUnblockCondition =
    gate.nextUnblockCondition ||
    (priority === "none"
      ? "none (entries currently eligible)"
      : "await gate condition recovery");
  lines.push(`• Next unblock: ${resolvedUnblockCondition}`);
}

function pushCapacityLines(lines: string[], thresholds: ThresholdSnapshot): void {
  if (thresholds.accountTier) {
    lines.push(`• Account tier: ${thresholds.accountTier}`);
  }
  if (thresholds.effectiveMaxConcurrentPositions !== undefined) {
    const managedOpen =
      thresholds.managedOpenPositionsEffective !== undefined
        ? thresholds.managedOpenPositionsEffective
        : "unknown";
    lines.push(
      `• Position cap: ${managedOpen}/${thresholds.effectiveMaxConcurrentPositions} managed open`,
    );
  }
  if (
    thresholds.effectiveMinConfidence !== undefined &&
    thresholds.effectiveMaxCapitalPct !== undefined
  ) {
    lines.push(
      `• Effective thresholds: min_confidence=${thresholds.effectiveMinConfidence.toFixed(2)}, max_capital=${thresholds.effectiveMaxCapitalPct.toFixed(2)}%`,
    );
  }
}

function pushFunnelLines(lines: string[], candidates: CandidateSnapshot): void {
  if (
    candidates.universe !== undefined &&
    candidates.ranked !== undefined &&
    candidates.viable !== undefined
  ) {
    lines.push(
      `• Candidate funnel: universe=${candidates.universe}, ranked=${candidates.ranked}, viable=${candidates.viable}`,
    );
  }
  candidates.rejections.slice(0, 3).forEach((rejection, index) => {
    const symbol = rejection.symbol;
    const reason = rejection.reason;
    if (symbol && reason) {
      lines.push(`• Top reject ${index + 1}: ${symbol} (${reason})`);
    }
  });
}

function pushProgressLines(
  lines: string[],
  rollout: RolloutSnapshot,
  attempts: AttemptSnapshot,
  execution: ExecutionSnapshot,
): void {
  if (rollout.progressBlocked) {
    lines.push(
      `• Progress watchdog: blocked${rollout.progressBlockReason ? ` (${rollout.progressBlockReason})` : ""}`,
    );
  }
  if (attempts.lastEntryAttemptAt) {
    const minutesText =
      attempts.minutesSinceEntryAttempt !== undefined
        ? ` (${attempts.minutesSinceEntryAttempt.toFixed(1)}m ago)`
        : "";
    lines.push(
      `• Last entry attempt: ${attempts.lastEntryAttemptAt}${minutesText}`,
    );
  }
  if (
    attempts.driftDeadlockCycles !== undefined &&
    attempts.driftDeadlockCycles > 0
  ) {
    lines.push(`• Drift deadlock cycles: ${attempts.driftDeadlockCycles}`);
  }
  if (attempts.driftSignature) {
    lines.push(`• Drift signature: ${attempts.driftSignature}`);
  }
  if (execution.stage) {
    lines.push(`• Execution stage: ${execution.stage}`);
  }
  if (execution.lastProgressAt) {
    const ageText =
      execution.inProgressAgeSeconds !== undefined
        ? ` (${execution.inProgressAgeSeconds.toFixed(1)}s ago)`
        : "";
    lines.push(
      `• Last execution progress: ${execution.lastProgressAt}${ageText}`,
    );
  }
}

function pushRecoveryLines(
  lines: string[],
  diagnostics: QuestDiagnosticsResponse,
  chatRuntime: ChatRuntimeState | undefined,
): void {
  const recoveryMode =
    diagnostics.recovery_mode || chatRuntime?.recovery_mode || "normal";
  const recoveryCleanCycles =
    diagnostics.recovery_clean_cycles_current ??
    chatRuntime?.recovery_clean_cycles_current ??
    0;
  const recoveryCleanRequired =
    diagnostics.recovery_clean_cycles_required ??
    chatRuntime?.recovery_clean_cycles_required ??
    1;
  const recoveryCyclesToEntry =
    diagnostics.recovery_cycles_to_entry ??
    chatRuntime?.recovery_cycles_to_entry ??
    0;
  const recoveryEntryAllowed =
    diagnostics.recovery_entry_allowed ??
    chatRuntime?.recovery_entry_allowed ??
    true;
  lines.push(
    `• Recovery: mode=${recoveryMode}, clean_cycles=${recoveryCleanCycles}/${recoveryCleanRequired}, entry_allowed=${recoveryEntryAllowed ? "yes" : "no"}`,
  );
  lines.push(`• Recovery cycles-to-entry: ${recoveryCyclesToEntry}`);
  const recoveryGateEvalAt =
    diagnostics.recovery_gate_eval_at || chatRuntime?.recovery_gate_eval_at;
  if (recoveryGateEvalAt) {
    lines.push(`• Recovery gate eval: ${recoveryGateEvalAt}`);
  }
}

function pushRecentEventLines(
  lines: string[],
  diagnostics: QuestDiagnosticsResponse,
  chatRuntime: ChatRuntimeState | undefined,
): void {
  const lastDriftRepair =
    chatRuntime?.last_drift_repair_at || diagnostics.last_drift_repair_at;
  if (lastDriftRepair) {
    lines.push(`• Last drift repair: ${lastDriftRepair}`);
  }

  const lastCleanReconcile =
    chatRuntime?.last_clean_reconcile_at || diagnostics.last_clean_reconcile_at;
  if (lastCleanReconcile) {
    lines.push(`• Last clean reconcile: ${lastCleanReconcile}`);
  }

  const lastReconcile = chatRuntime?.last_startup_reconcile;
  if (lastReconcile) {
    lines.push(`• Last startup reconcile: ${lastReconcile}`);
  }

  const lastSpotUnwind = chatRuntime?.last_spot_unwind;
  if (lastSpotUnwind) {
    lines.push(`• Last spot unwind: ${lastSpotUnwind}`);
  }
}

function pushAiRuntimeLines(
  lines: string[],
  aiRuntime: AiRuntimeState,
  chatRuntime: ChatRuntimeState | undefined,
): void {
  const runtimeStatus = aiRuntime.status?.toUpperCase() || "UNKNOWN";
  const providerChainUsable =
    aiRuntime.provider_chain_usable ?? chatRuntime?.provider_chain_usable;
  const providerChainConfigured =
    aiRuntime.provider_chain_configured ??
    chatRuntime?.provider_chain_configured;
  const lastSuccessProvider = aiRuntime.last_success_provider;
  const segments = [`${runtimeStatus}`];
  if (aiRuntime.error_rate !== undefined) {
    segments.push(`err_rate ${(aiRuntime.error_rate * 100).toFixed(0)}%`);
  }
  if (aiRuntime.circuit_active === true) {
    segments.push("circuit OPEN");
  }
  if (
    providerChainUsable !== undefined &&
    providerChainConfigured !== undefined
  ) {
    segments.push(
      `providers ${providerChainUsable}/${providerChainConfigured}`,
    );
  }
  if (lastSuccessProvider) {
    segments.push(`last_ok ${lastSuccessProvider}`);
  }
  lines.push(`• AI runtime: ${segments.join(", ")}`);
}

function pushQuestDiagnosticsLines(
  lines: string[],
  diagnostics: QuestDiagnosticsResponse | undefined,
): void {
  if (!diagnostics) {
    return;
  }
  const questRuntime = diagnostics.quest_runtime;
  const chatRuntime = diagnostics.chat_runtime;
  const aiRuntime = chatRuntime?.ai_runtime;

  const heartbeatMode = diagnostics.heartbeat?.mode;
  if (heartbeatMode) {
    lines.push(`• Heartbeat: ${heartbeatMode}`);
  }

  const cadenceMode = questRuntime?.cadence_mode;
  if (cadenceMode) {
    lines.push(`• Quest cadence: ${cadenceMode}`);
  }

  const riskLock = questRuntime?.risk_lock_active;
  if (riskLock !== undefined) {
    lines.push(`• Risk lock: ${riskLock ? "ACTIVE" : "inactive"}`);
  }

  const driftActive =
    chatRuntime?.state_drift_active ?? diagnostics.state_drift_active ?? false;
  const driftCount =
    chatRuntime?.state_drift_positions ?? diagnostics.state_drift_positions;
  lines.push(
    `• Drift gate: ${driftActive ? "ACTIVE" : "inactive"}${
      driftCount !== undefined ? ` (${driftCount} mismatch)` : ""
    }`,
  );

  const gate = gateSnapshot(diagnostics, questRuntime, chatRuntime, aiRuntime);
  const candidates = candidateSnapshot(diagnostics, chatRuntime);
  const rollout = rolloutSnapshot(diagnostics, chatRuntime);
  const attempts = attemptSnapshot(diagnostics, chatRuntime);
  const execution = executionSnapshot(
    diagnostics,
    questRuntime,
    chatRuntime,
  );
  const thresholds = thresholdSnapshot(diagnostics, chatRuntime);
  const priority = entryGatePriorityFor(
    riskLock,
    driftActive,
    gate.entryGateType,
    gate.runtimeCircuitActive,
    rollout.gateReason,
  );

  pushEntryGateLines(lines, gate, rollout, priority, driftCount);

  if (attempts.entryAttempts1h !== undefined) {
    lines.push(`• Entry attempts (1h): ${attempts.entryAttempts1h}`);
  }
  pushCapacityLines(lines, thresholds);
  pushFunnelLines(lines, candidates);
  pushProgressLines(lines, rollout, attempts, execution);
  pushRecoveryLines(lines, diagnostics, chatRuntime);
  pushRecentEventLines(lines, diagnostics, chatRuntime);
  if (aiRuntime) {
    pushAiRuntimeLines(lines, aiRuntime, chatRuntime);
  }
}

function pushDoctorLines(
  lines: string[],
  doctorResult: PromiseSettledResult<DoctorResponse>,
): void {
  if (doctorResult.status === "fulfilled") {
    const doctor: DoctorResponse = doctorResult.value;
    const overall = normalizeStatus(doctor.overall_status);
    lines.push(
      `• Health: ${iconForStatus(overall)} ${overall.toUpperCase()}`,
      `• Autonomous: ${summarizeCheck(doctor.checks, "autonomous-mode")}`,
      `• Exchange: ${summarizeCheck(doctor.checks, "exchange-connection")}`,
    );
    if (doctor.checked_at) {
      lines.push(`• Last health check: ${doctor.checked_at}`);
    }
  } else {
    lines.push(
      `• Health: unavailable (${shortError(doctorResult.reason as ClassifiableError)})`,
    );
  }
}

function pushTradingLines(
  lines: string[],
  modeResult: PromiseSettledResult<TradingModeResponse>,
  portfolioResult: PromiseSettledResult<PortfolioResponse>,
): void {
  if (modeResult.status === "fulfilled") {
    lines.push(`• Mode: ${summarizeMode(modeResult.value)}`);
  } else {
    lines.push(
      `• Mode: unavailable (${shortError(modeResult.reason as ClassifiableError)})`,
    );
  }

  if (portfolioResult.status === "fulfilled") {
    const portfolio: PortfolioResponse = portfolioResult.value;
    lines.push(
      `• Equity: ${portfolio.total_equity}`,
      `• Exposure: ${portfolio.exposure ?? "0.00"}`,
      `• Open positions: ${portfolio.positions?.length ?? 0}`,
    );
    if (portfolio.positions_source) {
      lines.push(`• Position source: ${portfolio.positions_source}`);
    }
    if (portfolio.drift_detected !== undefined) {
      lines.push(
        `• Portfolio drift flag: ${portfolio.drift_detected ? "true" : "false"}`,
      );
    }
    if (portfolio.open_orders !== undefined) {
      lines.push(`• Open orders: ${portfolio.open_orders}`);
    }
  } else {
    lines.push(
      `• Portfolio: unavailable (${shortError(portfolioResult.reason as ClassifiableError)})`,
    );
  }
}

function pushAiLines(
  lines: string[],
  aiResult: PromiseSettledResult<AIStatusResponse>,
  logsResult: PromiseSettledResult<LogsResponse>,
): void {
  if (aiResult.status === "fulfilled") {
    lines.push(`• AI: ${summarizeAI(aiResult.value)}`);
  } else {
    lines.push(
      `• AI: unavailable (${shortError(aiResult.reason as ClassifiableError)})`,
    );
  }

  if (logsResult.status === "fulfilled") {
    const logs: LogsResponse = logsResult.value;
    if (logs.logs && logs.logs.length > 0) {
      const lastLog = logs.logs[0];
      lines.push(
        `• Last activity: ${lastLog.timestamp} (${lastLog.level})`,
      );
    }
  }
}

function warnRejectedResults(
  chatId: string | number,
  entries: readonly RejectedResultEntry[],
): void {
  for (const { label, result } of entries) {
    if (result.status === "rejected") {
      logger.warn(`[Status] ${label} unavailable while building status`, {
        chatId,
        error: shortError(result.reason as ClassifiableError),
      });
    }
  }
}

/**
 * Register a "/status" command handler on the provided bot that composes and sends a multi-section status summary.
 *
 * The registered handler queries backend services for account, runtime, trading, AI, logs, and diagnostics data, then formats a human-readable status report and replies in-chat. Partial data is tolerated (the handler will include available information and log warnings for any backend failures). The handler also replies with user-facing error messages when chat or user records are missing or when an unexpected error occurs.
 *
 * @param bot - Bot instance to register the command on
 * @param api - Backend API client used to fetch user, doctor, trading mode, portfolio, AI status, logs, and diagnostics
 */
export function registerStatusCommand(
  bot: StatusCommandBot,
  api: StatusCommandApi,
): void {
  bot.command("status", async (ctx) => {
    const chatId = ctx.chat?.id;
    const userId = ctx.from?.id;

    if (!chatId) {
      await ctx.reply("Unable to lookup status: missing chat information.");
      return;
    }

    try {
      const userResult = await api.getUserByChatId(String(chatId));

      if (!userResult) {
        await ctx.reply("User not found. Please use /start to register.");
        return;
      }

      const [
        preferenceResult,
        doctorResult,
        modeResult,
        portfolioResult,
        aiResult,
        logsResult,
        questDiagnosticsResult,
      ] = await Promise.allSettled([
        userId
          ? api.getNotificationPreference(String(userId))
          : Promise.resolve({ enabled: true }),
        api.getDoctor(String(chatId)),
        api.getTradingMode(String(chatId)),
        api.getPortfolio(String(chatId)),
        api.getAIStatus(String(chatId)),
        api.getLogs(String(chatId), 1),
        api.getQuestDiagnostics(String(chatId)),
      ]);

      const preference =
        preferenceResult.status === "fulfilled"
          ? preferenceResult.value
          : { enabled: true };
      const questDiagnostics =
        questDiagnosticsResult.status === "fulfilled"
          ? questDiagnosticsResult.value
          : undefined;

      const createdAt = new Date(
        userResult.user.created_at,
      ).toLocaleDateString();
      const tier = userResult.user.subscription_tier;
      const notificationStatus = preference.enabled ? "Active" : "Paused";
      const lines = [
        "📊 Account Status",
        "",
        `💰 Subscription: ${tier}`,
        `📅 Member since: ${createdAt}`,
        `🔔 Notifications: ${notificationStatus}`,
        "",
        "🩺 Runtime Snapshot",
      ];

      pushDoctorLines(lines, doctorResult);
      pushQuestDiagnosticsLines(lines, questDiagnostics);

      lines.push("", "⚙️ Trading Snapshot");

      pushTradingLines(lines, modeResult, portfolioResult);

      lines.push("", "🤖 AI Snapshot");

      pushAiLines(lines, aiResult, logsResult);

      lines.push(
        "",
        "ℹ️ No recent trade messages does not always mean stopped. Use /doctor for detailed diagnostics.",
      );

      await ctx.reply(lines.join("\n"));

      warnRejectedResults(chatId, [
        { label: "Doctor", result: doctorResult },
        { label: "Mode", result: modeResult },
        { label: "Portfolio", result: portfolioResult },
        { label: "AI status", result: aiResult },
        { label: "Logs", result: logsResult },
        { label: "Quest diagnostics", result: questDiagnosticsResult },
      ]);
    } catch (error) {
      logger.error(
        "[Status] Failed to build /status response",
        error instanceof Error ? error : new Error(String(error)),
        { chatId, userId },
      );
      await ctx.reply("Unable to fetch status. Please try again later.");
    }
  });
}
