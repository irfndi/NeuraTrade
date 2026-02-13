/**
 * User data returned from backend API.
 * Retrieved via GET /api/v1/telegram/internal/users/:chatId
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
 * Retrieved via GET /api/v1/telegram/internal/notifications/:userId
 */
export interface NotificationPreferenceResponse {
  readonly enabled: boolean;
}

/**
 * Request body for setting notification preference.
 * Sent via POST /api/v1/telegram/internal/notifications/:userId
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
  readonly positions: readonly PortfolioPosition[];
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

export interface QuestsResponse {
  readonly quests: readonly QuestProgress[];
  readonly updated_at?: string;
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

export interface AIModelSelectResponse {
  readonly success: boolean;
  readonly model?: AIModelInfo;
  readonly message?: string;
}

export interface AIStatusResponse {
  readonly selected_model?: string;
  readonly provider?: string;
  readonly daily_spend?: string;
  readonly monthly_spend?: string;
  readonly budget_limit?: string;
  readonly daily_budget_exceeded?: boolean;
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
    `/api/v1/telegram/internal/users/${encodeURIComponent(chatId)}`,
  GET_NOTIFICATION_PREFERENCE: (userId: string) =>
    `/api/v1/telegram/internal/notifications/${encodeURIComponent(userId)}`,
  SET_NOTIFICATION_PREFERENCE: (userId: string) =>
    `/api/v1/telegram/internal/notifications/${encodeURIComponent(userId)}`,
  REGISTER_USER: "/api/v1/users/register",
  GET_ARBITRAGE_OPPORTUNITIES: (limit = 5, minProfit = 0.5) =>
    `/api/v1/arbitrage/opportunities?limit=${limit}&min_profit=${minProfit}`,
  BEGIN_AUTONOMOUS: "/api/v1/telegram/internal/autonomous/begin",
  PAUSE_AUTONOMOUS: "/api/v1/telegram/internal/autonomous/pause",
  GET_SUMMARY: (chatId: string, timeframe = "24h") =>
    `/api/v1/telegram/internal/performance/summary?chat_id=${encodeURIComponent(chatId)}&timeframe=${encodeURIComponent(timeframe)}`,
  GET_PERFORMANCE: (chatId: string, timeframe = "24h") =>
    `/api/v1/telegram/internal/performance?chat_id=${encodeURIComponent(chatId)}&timeframe=${encodeURIComponent(timeframe)}`,
  LIQUIDATE: "/api/v1/telegram/internal/liquidate",
  LIQUIDATE_ALL: "/api/v1/telegram/internal/liquidate/all",
  CONNECT_EXCHANGE: "/api/v1/telegram/internal/wallets/connect_exchange",
  CONNECT_POLYMARKET: "/api/v1/telegram/internal/wallets/connect_polymarket",
  ADD_WALLET: "/api/v1/telegram/internal/wallets",
  REMOVE_WALLET: "/api/v1/telegram/internal/wallets/remove",
  GET_QUESTS: (chatId: string) =>
    `/api/v1/telegram/internal/quests?chat_id=${encodeURIComponent(chatId)}`,
  GET_PORTFOLIO: (chatId: string) =>
    `/api/v1/telegram/internal/portfolio?chat_id=${encodeURIComponent(chatId)}`,
  GET_WALLETS: (chatId: string) =>
    `/api/v1/telegram/internal/wallets?chat_id=${encodeURIComponent(chatId)}`,
  GET_LOGS: (chatId: string, limit = 10) =>
    `/api/v1/telegram/internal/logs?chat_id=${encodeURIComponent(chatId)}&limit=${limit}`,
  GET_DOCTOR: (chatId: string) =>
    `/api/v1/telegram/internal/doctor?chat_id=${encodeURIComponent(chatId)}`,
  GET_AI_MODELS: "/api/v1/ai/models",
  SELECT_AI_MODEL: (userId: string) =>
    `/api/v1/ai/select/${encodeURIComponent(userId)}`,
  GET_AI_STATUS: (userId: string) =>
    `/api/v1/ai/status/${encodeURIComponent(userId)}`,
  ROUTE_AI_MODEL: "/api/v1/ai/route",
} as const;
