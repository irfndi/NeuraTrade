import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";
import type {
  AIStatusResponse,
  DoctorCheckResponse,
  DoctorResponse,
  LogsResponse,
  PortfolioResponse,
  QuestDiagnosticsResponse,
  TradingModeResponse,
} from "../api/types";
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
  return `${modeLabel} (${mode.confirmations}/${mode.required_confirmations} confirmations)`;
}

function summarizeAI(ai: AIStatusResponse): string {
  const model = ai.selected_model || "none selected";
  const provider = ai.provider || "n/a";
  const budget = ai.daily_budget_exceeded ? "budget exceeded" : "budget ok";
  return `${model} (${provider}, ${budget})`;
}

function shortError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function readStringField(
  source: Readonly<Record<string, unknown>> | undefined,
  key: string,
): string | null {
  if (!source) {
    return null;
  }
  const value = source[key];
  if (typeof value === "string" && value.trim().length > 0) {
    return value.trim();
  }
  return null;
}

function readBoolField(
  source: Readonly<Record<string, unknown>> | undefined,
  key: string,
): boolean | null {
  if (!source) {
    return null;
  }
  const value = source[key];
  if (typeof value === "boolean") {
    return value;
  }
  return null;
}

function readNumberField(
  source: Readonly<Record<string, unknown>> | undefined,
  key: string,
): number | null {
  if (!source) {
    return null;
  }
  const value = source[key];
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  return null;
}

function readRecordField(
  source: Readonly<Record<string, unknown>> | undefined,
  key: string,
): Readonly<Record<string, unknown>> | null {
  if (!source) {
    return null;
  }
  const value = source[key];
  if (typeof value === "object" && value !== null) {
    return value as Readonly<Record<string, unknown>>;
  }
  return null;
}

function readRecordArrayField(
  source: Readonly<Record<string, unknown>> | undefined,
  key: string,
): readonly Readonly<Record<string, unknown>>[] {
  if (!source) {
    return [];
  }
  const value = source[key];
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter(
    (item): item is Readonly<Record<string, unknown>> =>
      typeof item === "object" && item !== null,
  );
}

export function registerStatusCommand(bot: Bot, api: BackendApiClient): void {
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
        typeof (
          api as {
            getQuestDiagnostics?: (
              chatId: string,
            ) => Promise<QuestDiagnosticsResponse>;
          }
        ).getQuestDiagnostics === "function"
          ? api.getQuestDiagnostics(String(chatId))
          : Promise.reject(new Error("quest diagnostics unavailable")),
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
          `• Health: unavailable (${shortError(doctorResult.reason)})`,
        );
      }
      if (questDiagnosticsResult.status === "fulfilled") {
        const diagnostics = questDiagnosticsResult.value;
        const heartbeat = diagnostics.heartbeat as
          | Readonly<Record<string, unknown>>
          | undefined;
        const questRuntime = diagnostics.quest_runtime as
          | Readonly<Record<string, unknown>>
          | undefined;
        const chatRuntime = diagnostics.chat_runtime as
          | Readonly<Record<string, unknown>>
          | undefined;
        const aiRuntime =
          readRecordField(chatRuntime, "ai_runtime") ?? undefined;

        const heartbeatMode = readStringField(heartbeat, "mode");
        if (heartbeatMode) {
          lines.push(`• Heartbeat: ${heartbeatMode}`);
        }

        const cadenceMode = readStringField(questRuntime, "cadence_mode");
        if (cadenceMode) {
          lines.push(`• Quest cadence: ${cadenceMode}`);
        }

        const riskLock = readBoolField(questRuntime, "risk_lock_active");
        if (riskLock !== null) {
          lines.push(`• Risk lock: ${riskLock ? "ACTIVE" : "inactive"}`);
        }

        const driftActive =
          readBoolField(chatRuntime, "state_drift_active") ??
          diagnostics.state_drift_active ??
          false;
        const driftCount =
          readNumberField(chatRuntime, "state_drift_positions") ??
          diagnostics.state_drift_positions;
        lines.push(
          `• Drift gate: ${driftActive ? "ACTIVE" : "inactive"}${
            typeof driftCount === "number" ? ` (${driftCount} mismatch)` : ""
          }`,
        );
        const entryGateReason =
          readStringField(chatRuntime, "entry_gate_reason_current") ||
          diagnostics.entry_gate_reason_current;
        const entryGateType =
          readStringField(chatRuntime, "entry_gate_type") || "none";
        const riskLockSource =
          readStringField(chatRuntime, "risk_lock_source") ||
          readStringField(questRuntime, "risk_lock_source") ||
          diagnostics.risk_lock_source ||
          "none";
        const runtimeCircuitActive =
          readBoolField(aiRuntime, "circuit_active") === true;
        const entryAttemptBlockReason =
          readStringField(chatRuntime, "entry_attempt_block_reason") ||
          diagnostics.entry_attempt_block_reason;
        const nextUnblockCondition =
          readStringField(chatRuntime, "next_unblock_condition_current") ||
          diagnostics.next_unblock_condition_current;
        const accountTier =
          readStringField(chatRuntime, "account_tier") ||
          diagnostics.account_tier;
        const effectiveMinConfidence =
          readNumberField(chatRuntime, "effective_min_confidence") ??
          diagnostics.effective_min_confidence;
        const effectiveMaxCapitalPct =
          readNumberField(chatRuntime, "effective_max_capital_pct") ??
          diagnostics.effective_max_capital_pct;
        const candidateUniverseCount =
          readNumberField(chatRuntime, "candidate_universe_count") ??
          diagnostics.candidate_universe_count;
        const candidateRankedCount =
          readNumberField(chatRuntime, "candidate_ranked_count") ??
          diagnostics.candidate_ranked_count;
        const candidateViableCount =
          readNumberField(chatRuntime, "candidate_viable_count") ??
          diagnostics.candidate_viable_count;
        const topCandidateRejections =
          readRecordArrayField(chatRuntime, "top_candidate_rejections").length > 0
            ? readRecordArrayField(chatRuntime, "top_candidate_rejections")
            : (diagnostics.top_candidate_rejections ?? []);
        const progressBlocked =
          readBoolField(chatRuntime, "progress_blocked") ??
          diagnostics.progress_blocked ??
          false;
        const progressBlockReason =
          readStringField(chatRuntime, "progress_block_reason") ||
          diagnostics.progress_block_reason;
        const rolloutStageCurrent =
          readStringField(chatRuntime, "rollout_stage_current") ||
          diagnostics.rollout_stage_current;
        const rolloutStatusCurrent =
          readStringField(chatRuntime, "rollout_status_current") ||
          diagnostics.rollout_status_current;
        const rolloutGateReasonCurrent =
          readStringField(chatRuntime, "rollout_gate_reason_current") ||
          diagnostics.rollout_gate_reason_current;
        const entryAttempts1h =
          readNumberField(chatRuntime, "entry_attempts_1h") ??
          diagnostics.entry_attempts_1h;
        const lastEntryAttemptAt =
          readStringField(chatRuntime, "last_entry_attempt_at") ||
          diagnostics.last_entry_attempt_at;
        const minutesSinceEntryAttempt =
          readNumberField(chatRuntime, "minutes_since_entry_attempt") ??
          diagnostics.minutes_since_entry_attempt;
        const driftDeadlockCycles =
          readNumberField(chatRuntime, "drift_deadlock_cycles") ??
          diagnostics.drift_deadlock_cycles;
        const driftSignature =
          readStringField(chatRuntime, "drift_signature") ||
          diagnostics.drift_signature;
        const executionStage =
          readStringField(chatRuntime, "execution_stage") ||
          readStringField(questRuntime, "execution_stage");
        const executionLastProgressAt =
          readStringField(chatRuntime, "execution_last_progress_at") ||
          readStringField(questRuntime, "execution_last_progress_at");
        const executionInProgressAgeSeconds =
          readNumberField(chatRuntime, "execution_in_progress_age_seconds") ??
          readNumberField(questRuntime, "execution_in_progress_age_seconds");
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
            entryAttemptBlockReason === "rollout_shadow_block" ||
            (rolloutGateReasonCurrent &&
              typeof candidateViableCount === "number" &&
              candidateViableCount > 0)
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
        let blockerReason = entryGateReason || entryAttemptBlockReason || "";
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
                typeof driftCount === "number"
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
              blockerReason = "none";
              break;
          }
        }
        lines.push(`• Entry blocker: ${entryGatePriority} (${blockerReason})`);
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
        if (typeof entryAttempts1h === "number") {
          lines.push(`• Entry attempts (1h): ${entryAttempts1h}`);
        }
        if (accountTier) {
          lines.push(`• Account tier: ${accountTier}`);
        }
        if (
          typeof effectiveMinConfidence === "number" &&
          typeof effectiveMaxCapitalPct === "number"
        ) {
          lines.push(
            `• Effective thresholds: min_confidence=${effectiveMinConfidence.toFixed(2)}, max_capital=${effectiveMaxCapitalPct.toFixed(2)}%`,
          );
        }
        if (
          typeof candidateUniverseCount === "number" &&
          typeof candidateRankedCount === "number" &&
          typeof candidateViableCount === "number"
        ) {
          lines.push(
            `• Candidate funnel: universe=${candidateUniverseCount}, ranked=${candidateRankedCount}, viable=${candidateViableCount}`,
          );
        }
        topCandidateRejections.slice(0, 3).forEach((rejection, index) => {
          const symbol = readStringField(rejection, "symbol");
          const reason = readStringField(rejection, "reason");
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
            typeof minutesSinceEntryAttempt === "number"
              ? ` (${minutesSinceEntryAttempt.toFixed(1)}m ago)`
              : "";
          lines.push(
            `• Last entry attempt: ${lastEntryAttemptAt}${minutesText}`,
          );
        }
        if (
          typeof driftDeadlockCycles === "number" &&
          driftDeadlockCycles > 0
        ) {
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
            typeof executionInProgressAgeSeconds === "number"
              ? ` (${executionInProgressAgeSeconds.toFixed(1)}s ago)`
              : "";
          lines.push(
            `• Last execution progress: ${executionLastProgressAt}${ageText}`,
          );
        }

        const diagnosticsFields = diagnostics as unknown as Readonly<
          Record<string, unknown>
        >;
        const recoveryMode =
          readStringField(diagnosticsFields, "recovery_mode") ||
          readStringField(chatRuntime, "recovery_mode") ||
          "normal";
        const recoveryCleanCycles =
          readNumberField(diagnosticsFields, "recovery_clean_cycles_current") ??
          readNumberField(chatRuntime, "recovery_clean_cycles_current") ??
          0;
        const recoveryCleanRequired =
          readNumberField(
            diagnosticsFields,
            "recovery_clean_cycles_required",
          ) ??
          readNumberField(chatRuntime, "recovery_clean_cycles_required") ??
          1;
        const recoveryCyclesToEntry =
          readNumberField(diagnosticsFields, "recovery_cycles_to_entry") ??
          readNumberField(chatRuntime, "recovery_cycles_to_entry") ??
          0;
        const recoveryEntryAllowed =
          readBoolField(diagnosticsFields, "recovery_entry_allowed") ??
          readBoolField(chatRuntime, "recovery_entry_allowed") ??
          true;
        lines.push(
          `• Recovery: mode=${recoveryMode}, clean_cycles=${recoveryCleanCycles}/${recoveryCleanRequired}, entry_allowed=${recoveryEntryAllowed ? "yes" : "no"}`,
        );
        lines.push(`• Recovery cycles-to-entry: ${recoveryCyclesToEntry}`);
        const recoveryGateEvalAt =
          readStringField(diagnosticsFields, "recovery_gate_eval_at") ||
          readStringField(chatRuntime, "recovery_gate_eval_at");
        if (recoveryGateEvalAt) {
          lines.push(`• Recovery gate eval: ${recoveryGateEvalAt}`);
        }

        const lastDriftRepair =
          readStringField(chatRuntime, "last_drift_repair_at") ||
          diagnostics.last_drift_repair_at;
        if (lastDriftRepair) {
          lines.push(`• Last drift repair: ${lastDriftRepair}`);
        }

        const lastCleanReconcile =
          readStringField(chatRuntime, "last_clean_reconcile_at") ||
          diagnostics.last_clean_reconcile_at;
        if (lastCleanReconcile) {
          lines.push(`• Last clean reconcile: ${lastCleanReconcile}`);
        }

        const lastReconcile = readStringField(
          chatRuntime,
          "last_startup_reconcile",
        );
        if (lastReconcile) {
          lines.push(`• Last startup reconcile: ${lastReconcile}`);
        }

        const lastSpotUnwind = readStringField(chatRuntime, "last_spot_unwind");
        if (lastSpotUnwind) {
          lines.push(`• Last spot unwind: ${lastSpotUnwind}`);
        }
        if (aiRuntime) {
          const runtimeStatus =
            readStringField(aiRuntime, "status")?.toUpperCase() || "UNKNOWN";
          const errorRate = readNumberField(aiRuntime, "error_rate");
          const circuitActive = readBoolField(aiRuntime, "circuit_active");
          const providerChainUsable =
            readNumberField(aiRuntime, "provider_chain_usable") ??
            readNumberField(chatRuntime, "provider_chain_usable");
          const providerChainConfigured =
            readNumberField(aiRuntime, "provider_chain_configured") ??
            readNumberField(chatRuntime, "provider_chain_configured");
          const lastSuccessProvider = readStringField(
            aiRuntime,
            "last_success_provider",
          );
          const segments = [`${runtimeStatus}`];
          if (typeof errorRate === "number") {
            segments.push(`err_rate ${(errorRate * 100).toFixed(0)}%`);
          }
          if (circuitActive === true) {
            segments.push("circuit OPEN");
          }
          if (
            typeof providerChainUsable === "number" &&
            typeof providerChainConfigured === "number"
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
        lines.push(`• Mode: unavailable (${shortError(modeResult.reason)})`);
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
        if (typeof portfolio.drift_detected === "boolean") {
          lines.push(
            `• Portfolio drift flag: ${portfolio.drift_detected ? "true" : "false"}`,
          );
        }
        if (typeof portfolio.open_orders === "number") {
          lines.push(`• Open orders: ${portfolio.open_orders}`);
        }
      } else {
        lines.push(
          `• Portfolio: unavailable (${shortError(portfolioResult.reason)})`,
        );
      }

      lines.push("", "🤖 AI Snapshot");

      if (aiResult.status === "fulfilled") {
        lines.push(`• Model: ${summarizeAI(aiResult.value)}`);
      } else {
        lines.push(`• Model: unavailable (${shortError(aiResult.reason)})`);
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
          error: shortError(doctorResult.reason),
        });
      }
      if (modeResult.status === "rejected") {
        logger.warn("[Status] Mode unavailable while building status", {
          chatId,
          error: shortError(modeResult.reason),
        });
      }
      if (portfolioResult.status === "rejected") {
        logger.warn("[Status] Portfolio unavailable while building status", {
          chatId,
          error: shortError(portfolioResult.reason),
        });
      }
      if (aiResult.status === "rejected") {
        logger.warn("[Status] AI status unavailable while building status", {
          chatId,
          error: shortError(aiResult.reason),
        });
      }
      if (logsResult.status === "rejected") {
        logger.warn("[Status] Logs unavailable while building status", {
          chatId,
          error: shortError(logsResult.reason),
        });
      }
      if (questDiagnosticsResult.status === "rejected") {
        logger.warn(
          "[Status] Quest diagnostics unavailable while building status",
          {
            chatId,
            error: shortError(questDiagnosticsResult.reason),
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
