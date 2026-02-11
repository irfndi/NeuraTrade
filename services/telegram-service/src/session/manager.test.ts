import { describe, test, expect, beforeEach } from "bun:test";
import { SessionManager, createSessionManager } from "./manager";
import { isSessionExpired, createSessionState } from "./types";

describe("SessionManager", () => {
  let manager: SessionManager;

  beforeEach(() => {
    manager = new SessionManager();
  });

  describe("getSession", () => {
    test("returns null for non-existent session", () => {
      expect(manager.getSession("123")).toBeNull();
    });

    test("returns session after setting", () => {
      manager.setSession("123", { step: "awaiting_auth_email" });
      const session = manager.getSession("123");
      expect(session).not.toBeNull();
      expect(session?.step).toBe("awaiting_auth_email");
      expect(session?.chatId).toBe("123");
    });
  });

  describe("setSession", () => {
    test("creates new session", () => {
      const session = manager.setSession("123", {
        step: "awaiting_wallet_address",
        data: { exchange: "binance" },
      });
      expect(session.chatId).toBe("123");
      expect(session.step).toBe("awaiting_wallet_address");
      expect(session.data.exchange).toBe("binance");
    });

    test("updates existing session", () => {
      manager.setSession("123", { step: "awaiting_auth_email" });
      const updated = manager.setSession("123", { step: "awaiting_auth_password" });
      expect(updated.step).toBe("awaiting_auth_password");
      expect(updated.createdAt).toBe(updated.createdAt);
    });

    test("preserves data when updating step", () => {
      manager.setSession("123", { step: "awaiting_auth_email", data: { email: "test@example.com" } });
      const updated = manager.setSession("123", { step: "awaiting_auth_password" });
      expect(updated.data.email).toBe("test@example.com");
    });
  });

  describe("updateStep", () => {
    test("updates step for existing session", () => {
      manager.setSession("123", { step: "awaiting_auth_email" });
      const updated = manager.updateStep("123", "awaiting_auth_password");
      expect(updated?.step).toBe("awaiting_auth_password");
    });

    test("returns null for non-existent session", () => {
      expect(manager.updateStep("999", "awaiting_auth_email")).toBeNull();
    });
  });

  describe("setData", () => {
    test("merges data into existing session", () => {
      manager.setSession("123", { step: "awaiting_auth_email", data: { email: "test@example.com" } });
      const updated = manager.setData("123", { password: "secret" });
      expect(updated?.data.email).toBe("test@example.com");
      expect(updated?.data.password).toBe("secret");
    });

    test("returns null for non-existent session", () => {
      expect(manager.setData("999", { email: "test@example.com" })).toBeNull();
    });
  });

  describe("clearSession", () => {
    test("removes session", () => {
      manager.setSession("123", { step: "awaiting_auth_email" });
      manager.clearSession("123");
      expect(manager.getSession("123")).toBeNull();
    });

    test("does nothing for non-existent session", () => {
      manager.clearSession("999");
      expect(manager.getSession("999")).toBeNull();
    });
  });

  describe("isActive", () => {
    test("returns false for non-existent session", () => {
      expect(manager.isActive("123")).toBe(false);
    });

    test("returns false for idle session", () => {
      manager.setSession("123", { step: "idle" });
      expect(manager.isActive("123")).toBe(false);
    });

    test("returns true for active session", () => {
      manager.setSession("123", { step: "awaiting_auth_email" });
      expect(manager.isActive("123")).toBe(true);
    });
  });

  describe("getActiveSessionCount", () => {
    test("returns 0 when no sessions", () => {
      expect(manager.getActiveSessionCount()).toBe(0);
    });

    test("counts active sessions", () => {
      manager.setSession("1", { step: "awaiting_auth_email" });
      manager.setSession("2", { step: "awaiting_wallet_address" });
      manager.setSession("3", { step: "idle" });
      expect(manager.getActiveSessionCount()).toBe(2);
    });
  });

  describe("expiration", () => {
    test("expired session returns null", async () => {
      const shortTtlManager = new SessionManager(1);
      shortTtlManager.setSession("123", { step: "awaiting_auth_email" });
      
      await new Promise(resolve => setTimeout(resolve, 10));
      
      expect(shortTtlManager.getSession("123")).toBeNull();
    });

    test("cleanupExpired removes expired sessions", async () => {
      const shortTtlManager = new SessionManager(1);
      shortTtlManager.setSession("1", { step: "awaiting_auth_email" });
      shortTtlManager.setSession("2", { step: "awaiting_wallet_address" });
      
      await new Promise(resolve => setTimeout(resolve, 10));
      
      const cleaned = shortTtlManager.cleanupExpired();
      expect(cleaned).toBe(2);
      expect(shortTtlManager.getActiveSessionCount()).toBe(0);
    });
  });
});

describe("createSessionState", () => {
  test("creates session with defaults", () => {
    const session = createSessionState("123");
    expect(session.chatId).toBe("123");
    expect(session.step).toBe("idle");
    expect(session.data).toEqual({});
    expect(session.createdAt).toBeInstanceOf(Date);
    expect(session.expiresAt.getTime()).toBeGreaterThan(session.createdAt.getTime());
  });

  test("creates session with custom values", () => {
    const session = createSessionState("123", "awaiting_auth_email", { email: "test@example.com" });
    expect(session.step).toBe("awaiting_auth_email");
    expect(session.data.email).toBe("test@example.com");
  });
});

describe("isSessionExpired", () => {
  test("returns false for fresh session", () => {
    const session = createSessionState("123");
    expect(isSessionExpired(session)).toBe(false);
  });

  test("returns true for expired session", () => {
    const session = {
      ...createSessionState("123"),
      expiresAt: new Date(Date.now() - 1000),
    };
    expect(isSessionExpired(session)).toBe(true);
  });
});

describe("createSessionManager", () => {
  test("creates SessionManager instance", () => {
    const manager = createSessionManager();
    expect(manager).toBeInstanceOf(SessionManager);
  });

  test("accepts custom TTL", () => {
    const manager = createSessionManager(60000);
    expect(manager).toBeInstanceOf(SessionManager);
  });
});
