import {
  SessionState,
  SessionStep,
  SessionData,
  createSessionState,
  isSessionExpired,
  DEFAULT_SESSION_TTL_MS,
} from "./types";

export class SessionManager {
  private sessions: Map<string, SessionState> = new Map();
  private readonly ttlMs: number;

  constructor(ttlMs: number = DEFAULT_SESSION_TTL_MS) {
    this.ttlMs = ttlMs;
  }

  getSession(chatId: string): SessionState | null {
    const session = this.sessions.get(chatId);
    if (!session) {
      return null;
    }

    if (isSessionExpired(session)) {
      this.sessions.delete(chatId);
      return null;
    }

    return session;
  }

  setSession(
    chatId: string,
    partial: Partial<
      Omit<SessionState, "chatId" | "createdAt" | "updatedAt" | "expiresAt">
    >,
  ): SessionState {
    const existing = this.sessions.get(chatId);
    const now = new Date();

    let session: SessionState;
    if (existing && !isSessionExpired(existing)) {
      session = {
        ...existing,
        ...partial,
        chatId,
        createdAt: existing.createdAt,
        updatedAt: now,
        expiresAt: new Date(now.getTime() + this.ttlMs),
      };
    } else {
      session = createSessionState(
        chatId,
        partial.step ?? "idle",
        partial.data ?? {},
        this.ttlMs,
      );
    }

    this.sessions.set(chatId, session);
    return session;
  }

  updateStep(chatId: string, step: SessionStep): SessionState | null {
    const session = this.getSession(chatId);
    if (!session) {
      return null;
    }

    return this.setSession(chatId, { step, data: session.data });
  }

  setData(chatId: string, data: SessionData): SessionState | null {
    const session = this.getSession(chatId);
    if (!session) {
      return null;
    }

    return this.setSession(chatId, {
      step: session.step,
      data: { ...session.data, ...data },
    });
  }

  clearSession(chatId: string): void {
    this.sessions.delete(chatId);
  }

  isActive(chatId: string): boolean {
    const session = this.getSession(chatId);
    return session !== null && session.step !== "idle";
  }

  getActiveSessionCount(): number {
    let count = 0;
    for (const chatId of this.sessions.keys()) {
      if (this.isActive(chatId)) {
        count++;
      }
    }
    return count;
  }

  cleanupExpired(): number {
    let cleaned = 0;
    for (const [chatId, session] of this.sessions.entries()) {
      if (isSessionExpired(session)) {
        this.sessions.delete(chatId);
        cleaned++;
      }
    }
    return cleaned;
  }
}

export function createSessionManager(ttlMs?: number): SessionManager {
  return new SessionManager(ttlMs);
}
