import type {
  AIStatusResponse,
  DoctorCheckResponse,
  DoctorResponse,
  GetUserByChatIdResponse,
  LogsResponse,
  NotificationPreferenceResponse,
  PortfolioResponse,
  QuestDiagnosticsResponse,
  QuestRuntimeState,
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
      if (questDiagnosticsResult.status === "fulfilled") {
        const diagnostics = questDiagnosticsResult.value;
        const heartbeat = diagnostics.heartbeat;
        const questRuntime: QuestRuntimeState | undefined =
          diagnostics.quest_runtime;
        const chatRuntime = diagnostics.chat_runtime;
        const aiRuntime = chatRuntime?.ai_runtime;

        const heartbeatMode = heartbeat?.mode;
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
          chatRuntime?.state_drift_active ??
          diagnostics.state_drift_active ??
          false;
        const driftCount =
          chatRuntime?.state_drift_positions ??
          diagnostics.state_drift_positions;
        lines.push(
          `• Drift gate: ${driftActive ? "ACTIVE" : "inactive"}${
            driftCount !== undefined ? ` (${driftCount} mismatch)` : ""
          }`,
        );
        const entryGateReason =
          chatRuntime?.entry_gate_reason_current ||
          diagnostics.entry_gate_reason_current;
        const entryGateType = chatRuntime?.entry_gate_type || "none";
        const riskLockSource =
          chatRuntime?.risk_lock_source ||
          questRuntime?.risk_lock_source ||
          diagnostics.risk_lock_source ||
          "none";
        const runtimeCircuitActive = aiRuntime?.circuit_active === true;
        const entryAttemptBlockReason =
          chatRuntime?.entry_attempt_block_reason ||
          diagnostics.entry_attempt_block_reason;
        const nextUnblockCondition =
          chatRuntime?.next_unblock_condition_current ||
          diagnostics.next_unblock_condition_current;
        const accountTier =
          chatRuntime?.account_tier || diagnostics.account_tier;
        const effectiveMinConfidence =
          chatRuntime?.effective_min_confidence ??
          diagnostics.effective_min_confidence;
        const effectiveMaxCapitalPct =
          chatRuntime?.effective_max_capital_pct ??
          diagnostics.effective_max_capital_pct;
        const effectiveMaxConcurrentPositions =
          chatRuntime?.effective_max_concurrent_positions ??
          diagnostics.effective_max_concurrent_positions;
        const managedOpenPositionsEffective =
          chatRuntime?.managed_open_positions_effective ??
          diagnostics.managed_open_positions_effective;
        const candidateUniverseCount =
          chatRuntime?.candidate_universe_count ??
          diagnostics.candidate_universe_count;
        const candidateRankedCount =
          chatRuntime?.candidate_ranked_count ??
          diagnostics.candidate_ranked_count;
        const candidateViableCount =
          chatRuntime?.candidate_viable_count ??
          diagnostics.candidate_viable_count;
        const topCandidateRejections =
          chatRuntime?.top_candidate_rejections &&
          chatRuntime.top_candidate_rejections.length > 0
            ? chatRuntime.top_candidate_rejections
            : (diagnostics.top_candidate_rejections ?? []);
        const progressBlocked =
          chatRuntime?.progress_blocked ??
          diagnostics.progress_blocked ??
          false;
        const progressBlockReason =
          chatRuntime?.progress_block_reason ||
          diagnostics.progress_block_reason;
        const rolloutStageCurrent =
          chatRuntime?.rollout_stage_current ||
          diagnostics.rollout_stage_current;
        const rolloutStatusCurrent =
          chatRuntime?.rollout_status_current ||
          diagnostics.rollout_status_current;
        const rolloutGateReasonCurrent =
          chatRuntime?.rollout_gate_reason_current ||
          diagnostics.rollout_gate_reason_current;
        const entryAttempts1h =
          chatRuntime?.entry_attempts_1h ?? diagnostics.entry_attempts_1h;
        const lastEntryAttemptAt =
          chatRuntime?.last_entry_attempt_at ||
          diagnostics.last_entry_attempt_at;
        const minutesSinceEntryAttempt =
          chatRuntime?.minutes_since_entry_attempt ??
          diagnostics.minutes_since_entry_attempt;
        const driftDeadlockCycles =
          chatRuntime?.drift_deadlock_cycles ??
          diagnostics.drift_deadlock_cycles;
        const driftSignature =
          chatRuntime?.drift_signature || diagnostics.drift_signature;
        const executionStage =
          chatRuntime?.execution_stage || questRuntime?.execution_stage;
        const executionLastProgressAt =
          chatRuntime?.execution_last_progress_at ||
          questRuntime?.execution_last_progress_at;
        const executionInProgressAgeSeconds =
          chatRuntime?.execution_in_progress_age_seconds ??
          questRuntime?.execution_in_progress_age_seconds;
        const entryGatePriority = (() => {
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
        })();
        if (entryGateReason) {
          lines.push(`• Entry gate reason: ${entryGateReason}`);
        }
        // blockerReason is derived ONLY from hard gate priority, not candidate-level rejections.
        // Candidate rejections (entryAttemptBlockReason) are shown separately below.
        let blockerReason = entryGateReason || "";
        if (!blockerReason) {
          switch (entryGatePriority) {
            case "risk_lock":
              blockerReason =
                riskLockSource === "manual_env"
                  ? "forced by operator env lock"
                  : "global risk lock active";
              break;
            case "state_drift":
              blockerReason =
                driftCount !== undefined
                  ? `lifecycle drift pending reconcile (${driftCount})`
                  : "lifecycle drift pending reconcile";
              break;
            case "runtime_circuit":
              blockerReason = "AI runtime circuit is open";
              break;
            case "rollout_gate":
              blockerReason =
                rolloutGateReasonCurrent ||
                "strategy is not live yet; rollout still blocks execution";
              break;
            case "recovery_gate":
              blockerReason = "recovery clean-cycle gate active";
              break;
            default:
              blockerReason = "";
              break;
          }
        }
        // When no hard gate is active, show just "none" without confusing parenthetical.
        // Candidate-level rejections are displayed in the separate "Entry attempt block" line.
        let blockerDisplay = "none";
        if (entryGatePriority !== "none") {
          blockerDisplay = `${entryGatePriority}${blockerReason ? ` (${blockerReason})` : ""}`;
        }
        lines.push(`• Entry blocker: ${blockerDisplay}`);
        if (rolloutStageCurrent || rolloutStatusCurrent) {
          lines.push(
            `• Rollout: ${rolloutStageCurrent || "unknown"}/${rolloutStatusCurrent || "unknown"}`,
          );
        }
        if (rolloutGateReasonCurrent) {
          lines.push(`• Rollout gate: ${rolloutGateReasonCurrent}`);
        }
        if (entryGatePriority === "risk_lock") {
          lines.push(`• Risk lock source: ${riskLockSource}`);
        }
        if (entryAttemptBlockReason) {
          lines.push(`• Entry attempt block: ${entryAttemptBlockReason}`);
        }
        const resolvedUnblockCondition =
          nextUnblockCondition ||
          (entryGatePriority === "none"
            ? "none (entries currently eligible)"
            : "await gate condition recovery");
        lines.push(`• Next unblock: ${resolvedUnblockCondition}`);
        if (entryAttempts1h !== undefined) {
          lines.push(`• Entry attempts (1h): ${entryAttempts1h}`);
        }
        if (accountTier) {
          lines.push(`• Account tier: ${accountTier}`);
        }
        if (effectiveMaxConcurrentPositions !== undefined) {
          const managedOpen =
            managedOpenPositionsEffective !== undefined
              ? managedOpenPositionsEffective
              : "unknown";
          lines.push(
            `• Position cap: ${managedOpen}/${effectiveMaxConcurrentPositions} managed open`,
          );
        }
        if (
          effectiveMinConfidence !== undefined &&
          effectiveMaxCapitalPct !== undefined
        ) {
          lines.push(
            `• Effective thresholds: min_confidence=${effectiveMinConfidence.toFixed(2)}, max_capital=${effectiveMaxCapitalPct.toFixed(2)}%`,
          );
        }
        if (
          candidateUniverseCount !== undefined &&
          candidateRankedCount !== undefined &&
          candidateViableCount !== undefined
        ) {
          lines.push(
            `• Candidate funnel: universe=${candidateUniverseCount}, ranked=${candidateRankedCount}, viable=${candidateViableCount}`,
          );
        }
        topCandidateRejections.slice(0, 3).forEach((rejection, index) => {
          const symbol = rejection.symbol;
          const reason = rejection.reason;
          if (symbol && reason) {
            lines.push(`• Top reject ${index + 1}: ${symbol} (${reason})`);
          }
        });
        if (progressBlocked) {
          lines.push(
            `• Progress watchdog: blocked${progressBlockReason ? ` (${progressBlockReason})` : ""}`,
          );
        }
        if (lastEntryAttemptAt) {
          const minutesText =
            minutesSinceEntryAttempt !== undefined
              ? ` (${minutesSinceEntryAttempt.toFixed(1)}m ago)`
              : "";
          lines.push(
            `• Last entry attempt: ${lastEntryAttemptAt}${minutesText}`,
          );
        }
        if (driftDeadlockCycles !== undefined && driftDeadlockCycles > 0) {
          lines.push(`• Drift deadlock cycles: ${driftDeadlockCycles}`);
        }
        if (driftSignature) {
          lines.push(`• Drift signature: ${driftSignature}`);
        }
        if (executionStage) {
          lines.push(`• Execution stage: ${executionStage}`);
        }
        if (executionLastProgressAt) {
          const ageText =
            executionInProgressAgeSeconds !== undefined
              ? ` (${executionInProgressAgeSeconds.toFixed(1)}s ago)`
              : "";
          lines.push(
            `• Last execution progress: ${executionLastProgressAt}${ageText}`,
          );
        }

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
          diagnostics.recovery_gate_eval_at ||
          chatRuntime?.recovery_gate_eval_at;
        if (recoveryGateEvalAt) {
          lines.push(`• Recovery gate eval: ${recoveryGateEvalAt}`);
        }

        const lastDriftRepair =
          chatRuntime?.last_drift_repair_at || diagnostics.last_drift_repair_at;
        if (lastDriftRepair) {
          lines.push(`• Last drift repair: ${lastDriftRepair}`);
        }

        const lastCleanReconcile =
          chatRuntime?.last_clean_reconcile_at ||
          diagnostics.last_clean_reconcile_at;
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
        if (aiRuntime) {
          const runtimeStatus = aiRuntime.status?.toUpperCase() || "UNKNOWN";
          const errorRate = aiRuntime.error_rate;
          const circuitActive = aiRuntime.circuit_active;
          const providerChainUsable =
            aiRuntime.provider_chain_usable ??
            chatRuntime?.provider_chain_usable;
          const providerChainConfigured =
            aiRuntime.provider_chain_configured ??
            chatRuntime?.provider_chain_configured;
          const lastSuccessProvider = aiRuntime.last_success_provider;
          const segments = [`${runtimeStatus}`];
          if (errorRate !== undefined) {
            segments.push(`err_rate ${(errorRate * 100).toFixed(0)}%`);
          }
          if (circuitActive === true) {
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
      }

      lines.push("", "⚙️ Trading Snapshot");

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

      lines.push("", "🤖 AI Snapshot");

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

      lines.push(
        "",
        "ℹ️ No recent trade messages does not always mean stopped. Use /doctor for detailed diagnostics.",
      );

      await ctx.reply(lines.join("\n"));

      if (doctorResult.status === "rejected") {
        logger.warn("[Status] Doctor unavailable while building status", {
          chatId,
          error: shortError(doctorResult.reason as ClassifiableError),
        });
      }
      if (modeResult.status === "rejected") {
        logger.warn("[Status] Mode unavailable while building status", {
          chatId,
          error: shortError(modeResult.reason as ClassifiableError),
        });
      }
      if (portfolioResult.status === "rejected") {
        logger.warn("[Status] Portfolio unavailable while building status", {
          chatId,
          error: shortError(portfolioResult.reason as ClassifiableError),
        });
      }
      if (aiResult.status === "rejected") {
        logger.warn("[Status] AI status unavailable while building status", {
          chatId,
          error: shortError(aiResult.reason as ClassifiableError),
        });
      }
      if (logsResult.status === "rejected") {
        logger.warn("[Status] Logs unavailable while building status", {
          chatId,
          error: shortError(logsResult.reason as ClassifiableError),
        });
      }
      if (questDiagnosticsResult.status === "rejected") {
        logger.warn(
          "[Status] Quest diagnostics unavailable while building status",
          {
            chatId,
            error: shortError(
              questDiagnosticsResult.reason as ClassifiableError,
            ),
          },
        );
      }
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
