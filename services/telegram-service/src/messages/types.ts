export type RiskSeverity = "warning" | "critical";

export type DiagnosticStatus = "healthy" | "warning" | "critical";

export interface ActionAlertTemplateInput {
  readonly action: "BUY" | "SELL" | "LIQUIDATE" | string;
  readonly asset: string;
  readonly price: string;
  readonly size: string;
  readonly strategy: string;
  readonly reasoning?: string;
  readonly riskCheckPassed: boolean;
  readonly questId?: string;
  readonly timestamp?: string;
  readonly exchange?: string;
}

export interface QuestProgressTemplateInput {
  readonly questId: string;
  readonly questName: string;
  readonly current: number;
  readonly target: number;
  readonly percent?: number;
  readonly timeRemaining?: string;
  readonly status?: string;
}

export interface MilestoneAlertTemplateInput {
  readonly amount: string;
  readonly phase: string;
  readonly nextTarget?: string;
  readonly timestamp?: string;
  readonly note?: string;
}

export interface RiskEventAlertTemplateInput {
  readonly eventType: string;
  readonly severity: RiskSeverity;
  readonly message: string;
  readonly details?: Readonly<Record<string, string>>;
  readonly actionRequired?: string;
  readonly timestamp?: string;
}

export interface PerformanceSummaryTemplateInput {
  readonly timeframe: string;
  readonly pnl: string;
  readonly winRate?: string;
  readonly sharpe?: string;
  readonly drawdown?: string;
  readonly trades?: number;
  readonly bestTrade?: string;
  readonly worstTrade?: string;
  readonly note?: string;
}

export interface DoctorCheck {
  readonly name: string;
  readonly status: DiagnosticStatus;
  readonly message?: string;
  readonly latencyMs?: number;
  readonly details?: Readonly<Record<string, string>>;
}

export interface DoctorDiagnosticsTemplateInput {
  readonly overallStatus: DiagnosticStatus;
  readonly summary?: string;
  readonly checkedAt?: string;
  readonly checks: readonly DoctorCheck[];
}
