import type {
  ActionAlertTemplateInput,
  DoctorCheck,
  DoctorDiagnosticsTemplateInput,
  MilestoneAlertTemplateInput,
  PerformanceSummaryTemplateInput,
  QuestProgressTemplateInput,
  RiskEventAlertTemplateInput,
} from "./types";

const hasValue = (value?: string): boolean =>
  Boolean(value && value.trim().length > 0);

const formatSortedDetails = (
  details?: Readonly<Record<string, string>>,
): string[] => {
  if (!details) {
    return [];
  }

  return Object.keys(details)
    .sort((a, b) => a.localeCompare(b))
    .filter((key) => hasValue(details[key]))
    .map((key) => `• ${key}: ${details[key]}`);
};

const statusIcon = (status: "healthy" | "warning" | "critical"): string => {
  if (status === "healthy") {
    return "✅";
  }
  if (status === "warning") {
    return "⚠️";
  }
  return "🚨";
};

const riskIcon = (severity: "warning" | "critical"): string =>
  severity === "critical" ? "🚨" : "⚠️";

const normalizePercent = (input: QuestProgressTemplateInput): number => {
  if (typeof input.percent === "number") {
    return input.percent;
  }

  if (input.target <= 0) {
    return 0;
  }

  return Math.round((input.current / input.target) * 100);
};

const formatDoctorCheck = (check: DoctorCheck): string[] => {
  const impactRaw = (check.impact || "").toLowerCase();
  const impact =
    impactRaw === "core" || impactRaw === "optional"
      ? impactRaw
      : check.optional
        ? "optional"
        : "core";
  const lines = [
    `${statusIcon(check.status)} ${check.name} [${impact.toUpperCase()}]: ${check.status.toUpperCase()}`,
  ];

  if (hasValue(check.message)) {
    lines.push(`   ${check.message}`);
  }

  if (typeof check.latencyMs === "number") {
    lines.push(`   Latency: ${check.latencyMs}ms`);
  }

  const details = formatSortedDetails(check.details);
  for (const detail of details) {
    lines.push(`   ${detail}`);
  }

  return lines;
};

export const formatActionAlertMessage = (
  input: ActionAlertTemplateInput,
): string => {
  const lines: string[] = [`🤖 Action: ${input.action}`];

  if (hasValue(input.timestamp)) {
    lines.push(`Time: ${input.timestamp}`);
  }

  lines.push(`Asset: ${input.asset}`);

  if (hasValue(input.exchange)) {
    lines.push(`Exchange: ${input.exchange}`);
  }

  lines.push(`Price: ${input.price}`);
  lines.push(`Size: ${input.size}`);
  lines.push(`Strategy: ${input.strategy}`);
  lines.push(
    "Mode Safety: Execution follows the current /mode state; LIVE orders require backend proof-gate approval.",
  );

  if (hasValue(input.reasoning)) {
    lines.push("");
    lines.push(`Reasoning: ${input.reasoning}`);
  }

  lines.push("");
  lines.push(
    `Risk Check: ${input.riskCheckPassed ? "✅ PASSED" : "❌ FAILED"}`,
  );

  if (hasValue(input.questId)) {
    lines.push(`Quest: ${input.questId}`);
  }

  return lines.join("\n");
};

export const formatQuestProgressMessage = (
  input: QuestProgressTemplateInput,
): string => {
  const percent = normalizePercent(input);
  const lines = [
    `📋 Quest Progress`,
    ``,
    `${input.questName}`,
    `ID: ${input.questId}`,
    `Progress: ${input.current}/${input.target} (${percent}%)`,
  ];

  if (hasValue(input.timeRemaining)) {
    lines.push(`Time remaining: ${input.timeRemaining}`);
  }

  if (hasValue(input.status)) {
    lines.push(`Status: ${input.status}`);
  }

  return lines.join("\n");
};

export const formatMilestoneAlertMessage = (
  input: MilestoneAlertTemplateInput,
): string => {
  const lines = [
    `🎯 Fund Milestone Reached`,
    ``,
    `Amount: $${input.amount}`,
    `Phase: ${input.phase}`,
  ];

  if (hasValue(input.nextTarget)) {
    lines.push(`Next Target: $${input.nextTarget}`);
  }

  if (hasValue(input.timestamp)) {
    lines.push(`Time: ${input.timestamp}`);
  }

  if (hasValue(input.note)) {
    lines.push("");
    lines.push(`Note: ${input.note}`);
  }

  return lines.join("\n");
};

export const formatRiskEventMessage = (
  input: RiskEventAlertTemplateInput,
): string => {
  const lines = [
    `${riskIcon(input.severity)} ${input.severity.toUpperCase()}`,
    ``,
    `Type: ${input.eventType}`,
    ``,
    input.message,
  ];

  if (hasValue(input.actionRequired)) {
    lines.push("");
    lines.push(`Action Required: ${input.actionRequired}`);
  }

  if (hasValue(input.timestamp)) {
    lines.push(`Time: ${input.timestamp}`);
  }

  const details = formatSortedDetails(input.details);
  if (details.length > 0) {
    lines.push("");
    lines.push("Details:");
    lines.push(...details);
  }

  return lines.join("\n");
};

export const formatPerformanceSummaryMessage = (
  input: PerformanceSummaryTemplateInput,
): string => {
  const lines = [
    `📊 Performance Summary`,
    ``,
    `Timeframe: ${input.timeframe}`,
    `PnL: ${input.pnl}`,
  ];

  if (hasValue(input.winRate)) {
    lines.push(`Win Rate: ${input.winRate}`);
  }

  if (hasValue(input.sharpe)) {
    lines.push(`Sharpe: ${input.sharpe}`);
  }

  if (hasValue(input.drawdown)) {
    lines.push(`Max Drawdown: ${input.drawdown}`);
  }

  if (typeof input.trades === "number") {
    lines.push(`Trades: ${input.trades}`);
  }

  if (hasValue(input.bestTrade)) {
    lines.push(`Best Trade: ${input.bestTrade}`);
  }

  if (hasValue(input.worstTrade)) {
    lines.push(`Worst Trade: ${input.worstTrade}`);
  }

  if (hasValue(input.note)) {
    lines.push("");
    lines.push(`Note: ${input.note}`);
  }

  return lines.join("\n");
};

export const formatDoctorDiagnosticsMessage = (
  input: DoctorDiagnosticsTemplateInput,
): string => {
  const lines = [
    `${statusIcon(input.overallStatus)} Doctor`,
    ``,
    `Status: ${input.overallStatus.toUpperCase()}`,
  ];

  if (hasValue(input.checkedAt)) {
    lines.push(`Checked at: ${input.checkedAt}`);
  }

  if (hasValue(input.summary)) {
    lines.push(`Summary: ${input.summary}`);
  }

  if (input.checks.length > 0) {
    lines.push("");
    lines.push("Checks:");

    const sortedChecks = [...input.checks].sort((a, b) =>
      a.name.localeCompare(b.name),
    );

    for (const check of sortedChecks) {
      lines.push(...formatDoctorCheck(check));
    }
  }

  return lines.join("\n");
};
