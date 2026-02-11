export type SessionStep =
  | "idle"
  | "awaiting_auth_email"
  | "awaiting_auth_password"
  | "awaiting_wallet_address"
  | "awaiting_wallet_signature"
  | "awaiting_confirmation"
  | "awaiting_liquidation_confirm"
  | "awaiting_exchange_selection";

export interface SessionState {
  readonly chatId: string;
  readonly userId?: string;
  readonly step: SessionStep;
  readonly data: Record<string, unknown>;
  readonly createdAt: Date;
  readonly updatedAt: Date;
  readonly expiresAt: Date;
}

export interface SessionData {
  email?: string;
  walletAddress?: string;
  exchange?: string;
  amount?: number;
  action?: string;
  confirmationCode?: string;
  [key: string]: unknown;
}

export const DEFAULT_SESSION_TTL_MS = 30 * 60 * 1000;

export function createSessionState(
  chatId: string,
  step: SessionStep = "idle",
  data: SessionData = {},
  ttlMs: number = DEFAULT_SESSION_TTL_MS,
): SessionState {
  const now = new Date();
  return {
    chatId,
    step,
    data,
    createdAt: now,
    updatedAt: now,
    expiresAt: new Date(now.getTime() + ttlMs),
  };
}

export function isSessionExpired(session: SessionState): boolean {
  return new Date() > session.expiresAt;
}
