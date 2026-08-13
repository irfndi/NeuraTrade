/**
 * User data returned from backend API.
 * Retrieved via GET /internal/telegram/users/:chatId
 */
export interface BackendUser {
  readonly id: string;
  readonly subscription_tier: string;
  readonly created_at: string;
}

/**
 * Response wrapper for getUserByChatId endpoint.
 */
export interface GetUserByChatIdResponse {
  readonly user: BackendUser;
}

/**
 * Notification preference returned from backend API.
 * Retrieved via GET /internal/telegram/notifications/:userId
 */
export interface NotificationPreferenceResponse {
  readonly enabled: boolean;
}

/**
 * Request body for setting notification preference.
 * Sent via POST /internal/telegram/notifications/:userId
 */
export interface SetNotificationPreferenceRequest {
  readonly enabled: boolean;
}

/**
 * Request body for registering a new Telegram user.
 * Sent via POST /api/v1/users/register
 */
export interface RegisterTelegramUserRequest {
  readonly email: string;
  readonly password: string;
  readonly telegram_chat_id: string;
}

/**
 * Arbitrage opportunity data from backend API.
 * Part of GetArbitrageOpportunitiesResponse.
 */
export interface ArbitrageOpportunity {
  readonly symbol: string;
  readonly buy_exchange: string;
  readonly buy_price: number;
  readonly sell_exchange: string;
  readonly sell_price: number;
  readonly profit_percent: number;
}

/**
 * Response from arbitrage opportunities endpoint.
 * Retrieved via GET /api/v1/arbitrage/opportunities
 */
export interface GetArbitrageOpportunitiesResponse {
  readonly opportunities: readonly ArbitrageOpportunity[];
}

/**
 * Generic API error response structure.
 * Used when parsing error messages from failed API calls.
 */
export interface ApiErrorResponse {
  readonly error?: string;
  readonly message?: string;
}

/**
 * Health check response from telegram-service itself.
 * Used for readiness/liveness probes.
 */
export interface HealthCheckResponse {
  readonly status: "healthy";
  readonly service: "telegram-service";
}

/**
 * Send message request for internal HTTP endpoint.
 * Used by backend-api to deliver notifications via POST /send-message
 */
export interface SendMessageRequest {
  readonly chatId: string | number;
  readonly text: string;
  readonly parseMode?: "HTML" | "Markdown" | "MarkdownV2";
}

/**
 * Response from send-message endpoint.
 */
export interface SendMessageResponse {
  readonly ok: boolean;
}

export interface BeginAutonomousResponse {
  readonly ok: boolean;
  readonly status?: string;
  readonly mode?: string;
  readonly message?: string;
  readonly readiness_passed?: boolean;
  readonly failed_checks?: readonly string[];
}

export interface PauseAutonomousResponse {
  readonly ok: boolean;
  readonly status?: string;
  readonly message?: string;
}

export interface PerformanceSummaryResponse {
  readonly timeframe: string;
  readonly pnl: string;
  readonly win_rate?: string;
  readonly sharpe?: string;
  readonly drawdown?: string;
  readonly trades?: number;
  readonly best_trade?: string;
  readonly worst_trade?: string;
  readonly note?: string;
}

export interface StrategyPerformance {
  readonly strategy: string;
  readonly pnl: string;
  readonly win_rate?: string;
  readonly sharpe?: string;
  readonly drawdown?: string;
  readonly trades?: number;
}

export interface PerformanceBreakdownResponse {
  readonly timeframe: string;
  readonly overall: PerformanceSummaryResponse;
  readonly strategies: readonly StrategyPerformance[];
}

export interface LiquidationResponse {
  readonly ok: boolean;
  readonly message?: string;
  readonly liquidated_count?: number;
  readonly request_id?: string;
}

export interface WalletCommandResponse {
  readonly ok: boolean;
  readonly message?: string;
}

export interface PortfolioPosition {
  readonly symbol: string;
  readonly side: string;
  readonly size: string;
  readonly entry_price?: string;
  readonly mark_price?: string;
  readonly unrealized_pnl?: string;
}

export interface PortfolioResponse {
  readonly total_equity: string;
  readonly available_balance?: string;
  readonly exposure?: string;
  readonly open_orders?: number;
  readonly positions: readonly PortfolioPosition[];
  readonly drift_detected?: boolean;
  readonly positions_source?:
    | "exchange"
    | "lifecycle_repair_pending"
    | "lifecycle_fallback"
    | string;
  readonly note?: string;
  readonly updated_at?: string;
}

export interface QuestProgress {
  readonly quest_id: string;
  readonly quest_name: string;
  readonly current: number;
  readonly target: number;
  readonly percent?: number;
  readonly status?: string;
  readonly time_remaining?: string;
}

export interface QuestRuntimeState {
  readonly cadence_mode?: string;
  readonly risk_lock_active?: boolean;
  readonly risk_lock_source?: string;
  readonly execution_stage?: string;
  readonly execution_last_progress_at?: string;
  readonly execution_in_progress_age_seconds?: number;
}

export interface AiRuntimeState {
  readonly status?: string;
  readonly error_rate?: number;
  readonly circuit_active?: boolean;
  readonly provider_chain_usable?: number;
  readonly provider_chain_configured?: number;
  readonly last_success_provider?: string;
}

export interface TopCandidateRejection {
  readonly symbol?: string;
  readonly reason?: string;
}

export interface ChatRuntimeState {
  readonly ai_runtime?: AiRuntimeState;
  readonly state_drift_active?: boolean;
  readonly state_drift_positions?: number;
  readonly entry_gate_reason_current?: string;
  readonly entry_gate_type?: string;
  readonly risk_lock_source?: string;
  readonly entry_attempt_block_reason?: string;
  readonly next_unblock_condition_current?: string;
  readonly account_tier?: string;
  readonly effective_min_confidence?: number;
  readonly effective_max_capital_pct?: number;
  readonly effective_max_concurrent_positions?: number;
  readonly managed_open_positions_effective?: number;
  readonly candidate_universe_count?: number;
  readonly candidate_ranked_count?: number;
  readonly candidate_viable_count?: number;
  readonly top_candidate_rejections?: readonly TopCandidateRejection[];
  readonly progress_blocked?: boolean;
  readonly progress_block_reason?: string;
  readonly rollout_stage_current?: string;
  readonly rollout_status_current?: string;
  readonly rollout_gate_reason_current?: string;
  readonly entry_attempts_1h?: number;
  readonly last_entry_attempt_at?: string;
  readonly minutes_since_entry_attempt?: number;
  readonly drift_deadlock_cycles?: number;
  readonly drift_signature?: string;
  readonly execution_stage?: string;
  readonly execution_last_progress_at?: string;
  readonly execution_in_progress_age_seconds?: number;
  readonly recovery_mode?: string;
  readonly recovery_clean_cycles_current?: number;
  readonly recovery_clean_cycles_required?: number;
  readonly recovery_cycles_to_entry?: number;
  readonly recovery_entry_allowed?: boolean;
  readonly recovery_gate_eval_at?: string;
  readonly last_drift_repair_at?: string;
  readonly last_clean_reconcile_at?: string;
  readonly last_startup_reconcile?: string;
  readonly last_spot_unwind?: string;
  readonly provider_chain_usable?: number;
  readonly provider_chain_configured?: number;
}

export interface HeartbeatState {
  readonly mode?: string;
}

export interface QuestsResponse {
  readonly quests: readonly QuestProgress[];
  readonly updated_at?: string;
}

/** Runtime state of the autonomous trading loop, nested under quest diagnostics. */
export interface QuestRuntimeState {
  readonly cadence_mode?: string;
  readonly risk_lock_active?: boolean;
  readonly risk_lock_source?: string;
  readonly execution_stage?: string;
  readonly execution_last_progress_at?: string;
  readonly execution_in_progress_age_seconds?: number;
}

/** AI runtime state nested under chat runtime state. */
export interface AiRuntimeState {
  readonly status?: string;
  readonly error_rate?: number;
  readonly circuit_active?: boolean;
  readonly provider_chain_usable?: number;
  readonly provider_chain_configured?: number;
  readonly last_success_provider?: string;
}

/** A single candidate rejection recorded in the chat runtime state. */
export interface TopCandidateRejection {
  readonly symbol?: string;
  readonly reason?: string;
}

/** Chat-scoped runtime state nested under quest diagnostics. */
export interface ChatRuntimeState {
  readonly state_drift_active?: boolean;
  readonly state_drift_positions?: number;
  readonly entry_gate_reason_current?: string;
  readonly entry_gate_type?: string;
  readonly risk_lock_source?: string;
  readonly entry_attempt_block_reason?: string;
  readonly next_unblock_condition_current?: string;
  readonly account_tier?: string;
  readonly effective_min_confidence?: number;
  readonly effective_max_capital_pct?: number;
  readonly effective_max_concurrent_positions?: number;
  readonly managed_open_positions_effective?: number;
  readonly candidate_universe_count?: number;
  readonly candidate_ranked_count?: number;
  readonly candidate_viable_count?: number;
  readonly top_candidate_rejections?: readonly TopCandidateRejection[];
  readonly progress_blocked?: boolean;
  readonly progress_block_reason?: string;
  readonly rollout_stage_current?: string;
  readonly rollout_status_current?: string;
  readonly rollout_gate_reason_current?: string;
  readonly entry_attempts_1h?: number;
  readonly last_entry_attempt_at?: string;
  readonly minutes_since_entry_attempt?: number;
  readonly drift_deadlock_cycles?: number;
  readonly drift_signature?: string;
  readonly recovery_mode?: string;
  readonly recovery_clean_cycles_current?: number;
  readonly recovery_clean_cycles_required?: number;
  readonly recovery_cycles_to_entry?: number;
  readonly recovery_entry_allowed?: boolean;
  readonly recovery_gate_eval_at?: string;
  readonly last_drift_repair_at?: string;
  readonly last_clean_reconcile_at?: string;
  readonly last_startup_reconcile?: string;
  readonly last_spot_unwind?: string;
  readonly ai_runtime?: AiRuntimeState;
}

/** Heartbeat state nested under quest diagnostics. */
export interface HeartbeatState {
  readonly mode?: string;
}

export interface QuestDiagnosticsResponse {
  readonly chat_id?: string;
  readonly autonomous?: boolean;
  readonly started_at?: string;
  readonly state_drift_active?: boolean;
  readonly state_drift_positions?: number;
  readonly entry_gate_reason_current?: string;
  readonly entry_gate_type?:
    | "none"
    | "risk_lock"
    | "state_drift"
    | "runtime_circuit"
    | "rollout_gate"
    | "recovery_gate"
    | string;
  readonly risk_lock_source?:
    | "manual_env"
    | "portfolio_safety"
    | "drawdown_threshold"
    | "none"
    | string;
  readonly execution_stage?: "lock" | "handler" | "persist" | "done" | string;
  readonly execution_last_progress_at?: string;
  readonly execution_in_progress_age_seconds?: number;
  readonly entry_attempt_block_reason?: string;
  readonly next_unblock_condition_current?: string;
  readonly account_tier?: string;
  readonly effective_min_confidence?: number;
  readonly effective_max_capital_pct?: number;
  readonly effective_max_concurrent_positions?: number;
  readonly managed_open_positions_effective?: number;
  readonly candidate_universe_count?: number;
  readonly candidate_ranked_count?: number;
  readonly candidate_viable_count?: number;
  readonly top_candidate_rejections?: readonly TopCandidateRejection[];
  readonly progress_blocked?: boolean;
  readonly progress_block_reason?: string;
  readonly rollout_stage_current?: "shadow" | "paper" | "live" | string;
  readonly rollout_status_current?:
    | "active"
    | "paused"
    | "rolled_back"
    | string;
  readonly rollout_gate_reason_current?: string;
  readonly last_entry_attempt_at?: string;
  readonly minutes_since_entry_attempt?: number;
  readonly entry_attempts_1h?: number;
  readonly drift_signature?: string;
  readonly drift_deadlock_cycles?: number;
  readonly recovery_mode?: "normal" | "derisk_only" | "micro_entry" | string;
  readonly recovery_clean_cycles_current?: number;
  readonly recovery_clean_cycles_required?: number;
  readonly recovery_cycles_to_entry?: number;
  readonly recovery_gate_eval_at?: string;
  readonly recovery_entry_allowed?: boolean;
  readonly last_drift_repair_at?: string;
  readonly last_clean_reconcile_at?: string;
  readonly provider_chain_configured?: number;
  readonly provider_chain_usable?: number;
  readonly quest_runtime?: QuestRuntimeState;
  readonly chat_runtime?: ChatRuntimeState;
  readonly heartbeat?: HeartbeatState;
  readonly timestamp?: string;
}

export interface WalletInfo {
  readonly wallet_id?: string;
  readonly type: string;
  readonly provider: string;
  readonly address_masked: string;
  readonly status: string;
  readonly connected_at?: string;
}

export interface WalletsResponse {
  readonly wallets: readonly WalletInfo[];
}

export interface OperatorLogEntry {
  readonly timestamp: string;
  readonly level: string;
  readonly source?: string;
  readonly message: string;
}

export interface LogsResponse {
  readonly logs: readonly OperatorLogEntry[];
}

export interface DoctorCheckResponse {
  readonly name: string;
  readonly status: "healthy" | "warning" | "critical" | string;
  readonly message?: string;
  readonly latency_ms?: number;
  readonly details?: Readonly<Record<string, string>>;
  readonly optional?: boolean;
  readonly impact?: "core" | "optional" | string;
}

export interface DoctorResponse {
  readonly overall_status: "healthy" | "warning" | "critical" | string;
  readonly summary?: string;
  readonly checked_at?: string;
  readonly checks: readonly DoctorCheckResponse[];
}

export interface AIModelInfo {
  readonly model_id: string;
  readonly display_name: string;
  readonly provider: string;
  readonly supports_tools: boolean;
  readonly supports_vision: boolean;
  readonly supports_reasoning: boolean;
  readonly cost: string;
  readonly tier: string;
  readonly latency_class: string;
}

export interface AIModelsResponse {
  readonly models: readonly AIModelInfo[];
  readonly providers: readonly string[];
  readonly last_sync?: string;
}

export interface AIProviderModelsResponse {
  readonly provider: string;
  readonly models: readonly AIModelInfo[];
}

export interface AIModelSelectResponse {
  readonly success: boolean;
  readonly model?: AIModelInfo;
  readonly message?: string;
}

export interface AIProviderInfo {
  readonly id: string;
  readonly name: string;
  readonly is_active: boolean;
  readonly model_count?: number;
}

export interface AIProvidersResponse {
  readonly providers: readonly AIProviderInfo[];
}

export interface AIStatusResponse {
  readonly selected_model?: string;
  readonly provider?: string;
  readonly daily_spend?: string;
  readonly monthly_spend?: string;
  readonly budget_limit?: string;
  readonly daily_budget_exceeded?: boolean;
  readonly runtime_ready?: boolean;
  readonly provider_chain_configured?: number;
  readonly provider_chain_usable?: number;
  readonly effective_provider?: string;
  readonly effective_model?: string;
  readonly auto_routing?: boolean;
  readonly readiness?:
    | "ready"
    | "ready_auto_route"
    | "degraded"
    | "unavailable";
}

export interface AIRouteRequest {
  readonly latency_preference?: "fast" | "balanced" | "accurate";
  readonly require_tools?: boolean;
  readonly require_vision?: boolean;
  readonly require_reasoning?: boolean;
  readonly max_cost?: string;
  readonly allowed_providers?: readonly string[];
}

export interface AIRouteResponse {
  readonly model: AIModelInfo;
  readonly score?: number;
  readonly reason?: string;
  readonly alternatives?: readonly AIModelInfo[];
}

/**
 * Webhook update response.
 */
export interface WebhookUpdateResponse {
  readonly ok: boolean;
}

/**
 * API endpoint paths for backend communication.
 */
export const API_ENDPOINTS = {
  GET_USER_BY_CHAT_ID: (chatId: string) =>
    `/internal/telegram/users/${encodeURIComponent(chatId)}`,
  GET_NOTIFICATION_PREFERENCE: (userId: string) =>
    `/internal/telegram/notifications/${encodeURIComponent(userId)}`,
  SET_NOTIFICATION_PREFERENCE: (userId: string) =>
    `/internal/telegram/notifications/${encodeURIComponent(userId)}`,
  REGISTER_USER: "/api/v1/users/register",
  GET_ARBITRAGE_OPPORTUNITIES: (limit = 5, minProfit = 0.5) =>
    `/api/v1/arbitrage/opportunities?limit=${limit}&min_profit=${minProfit}`,
  BEGIN_AUTONOMOUS: "/internal/telegram/autonomous/begin",
  PAUSE_AUTONOMOUS: "/internal/telegram/autonomous/pause",
  GET_SUMMARY: (chatId: string, timeframe = "24h") =>
    `/api/v1/telegram/internal/performance/summary?chat_id=${encodeURIComponent(chatId)}&timeframe=${encodeURIComponent(timeframe)}`,
  GET_PERFORMANCE: (chatId: string, timeframe = "24h") =>
    `/api/v1/telegram/internal/performance?chat_id=${encodeURIComponent(chatId)}&timeframe=${encodeURIComponent(timeframe)}`,
  LIQUIDATE: "/api/v1/telegram/internal/liquidate",
  LIQUIDATE_ALL: "/api/v1/telegram/internal/liquidate/all",
  CONNECT_EXCHANGE: "/internal/telegram/wallets/connect_exchange",
  CONNECT_POLYMARKET: "/internal/telegram/wallets/connect_polymarket",
  ADD_WALLET: "/internal/telegram/wallets",
  REMOVE_WALLET: "/internal/telegram/wallets/remove",
  GET_QUESTS: (chatId: string) =>
    `/api/v1/telegram/internal/quests?chat_id=${encodeURIComponent(chatId)}`,
  GET_QUEST_DIAGNOSTICS: (chatId: string) =>
    `/api/v1/telegram/internal/quests/diagnostics?chat_id=${encodeURIComponent(chatId)}`,
  GET_PORTFOLIO: (chatId: string) =>
    `/api/v1/telegram/internal/portfolio?chat_id=${encodeURIComponent(chatId)}`,
  GET_WALLETS: (chatId: string) =>
    `/internal/telegram/wallets?chat_id=${encodeURIComponent(chatId)}`,
  GET_LOGS: (chatId: string, limit = 10) =>
    `/api/v1/telegram/internal/logs?chat_id=${encodeURIComponent(chatId)}&limit=${limit}`,
  GET_DOCTOR: (chatId: string) =>
    `/internal/telegram/doctor?chat_id=${encodeURIComponent(chatId)}`,
  GET_AI_MODELS: "/api/v1/ai/models",
  GET_AI_PROVIDERS: "/api/v1/ai/providers",
  GET_AI_PROVIDER_MODELS: (providerId: string) =>
    `/api/v1/ai/providers/${encodeURIComponent(providerId)}/models`,
  SELECT_AI_MODEL: (userId: string) =>
    `/api/v1/ai/select/${encodeURIComponent(userId)}`,
  GET_AI_STATUS: (chatId: string) =>
    `/api/v1/telegram/internal/ai/status/${encodeURIComponent(chatId)}`,
  ROUTE_AI_MODEL: "/api/v1/ai/route",
  GET_ALERTS: (userId: string) =>
    `/api/v1/alerts?user_id=${encodeURIComponent(userId)}`,
  CREATE_ALERT: "/api/v1/alerts",
  UPDATE_ALERT: (alertId: string) =>
    `/api/v1/alerts/${encodeURIComponent(alertId)}`,
  DELETE_ALERT: (alertId: string) =>
    `/api/v1/alerts/${encodeURIComponent(alertId)}`,
} as const;

/**
 * A single value held in an alert condition. Alerts carry arbitrary
 * JSON-like conditions, so the value is a recursive JSON-compatible type
 * rather than an untyped escape hatch.
 */
export type AlertConditionValue =
  | string
  | number
  | boolean
  | null
  | readonly AlertConditionValue[]
  | { readonly [key: string]: AlertConditionValue };

export interface UserAlert {
  readonly id: string;
  readonly user_id: string;
  readonly alert_type: string;
  readonly conditions: Record<string, AlertConditionValue>;
  readonly is_active: boolean;
  readonly created_at: string;
}

export interface GetAlertsResponse {
  readonly status: string;
  readonly data: readonly UserAlert[];
}

export interface CreateAlertRequest {
  readonly alert_type: string;
  readonly conditions: Record<string, AlertConditionValue>;
}

export interface CreateAlertResponse {
  readonly status: string;
  readonly message: string;
  readonly data: UserAlert;
}

/**
 * Trading mode response from backend API.
 */
export interface TradingModeResponse {
  readonly mode: "dry" | "live";
  readonly confirmations: number;
  readonly required_confirmations: number;
  readonly changed_at?: string;
  readonly changed_by?: string;
}

/**
 * Response from setting trading mode.
 */
export interface SetTradingModeResponse {
  readonly success: boolean;
  readonly mode: string;
  readonly error?: string;
}

/**
 * Response from adding trading mode confirmation.
 */
export interface TradingModeConfirmationResponse {
  readonly confirmations: number;
  readonly required: number;
  readonly error?: string;
}

// Add trading mode endpoints to API_ENDPOINTS (need to redeclare)
export const TRADING_MODE_ENDPOINTS = {
  GET_TRADING_MODE: (chatId: string) =>
    `/api/v1/telegram/internal/mode/${encodeURIComponent(chatId)}`,
  SET_TRADING_MODE: (chatId: string) =>
    `/api/v1/telegram/internal/mode/${encodeURIComponent(chatId)}`,
  ADD_CONFIRMATION: (chatId: string) =>
    `/api/v1/telegram/internal/mode/${encodeURIComponent(chatId)}/confirm`,
  RESET_CONFIRMATIONS: (chatId: string) =>
    `/api/v1/telegram/internal/mode/${encodeURIComponent(chatId)}/confirmations`,
} as const;
