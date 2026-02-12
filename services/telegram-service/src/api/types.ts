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
} as const;
