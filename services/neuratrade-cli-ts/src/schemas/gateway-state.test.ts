import { describe, expect, test } from "bun:test";
import { Result } from "effect";
import {
  GatewayStateSchema,
  decodeGatewayStateEither,
  type GatewayState,
  type GatewayServiceRuntime,
} from "./gateway-state";

describe("GatewayStateSchema", () => {
  // --- Valid JSON decoding ---

  test("decodes a complete gateway state", () => {
    const input = {
      mode: "native",
      supervised: true,
      updated_at: "2025-01-15T12:00:00Z",
      health_timeout_seconds: 150,
      services: {
        "neuratrade-server": {
          status: "running",
          detail: "healthy",
          endpoint: "http://localhost:8080",
        },
        "telegram-service": {
          status: "running",
        },
        "ccxt-service": {
          status: "stopped",
          detail: "skipped: native mode",
        },
      },
    };

    const result = decodeGatewayStateEither(input);
    expect(Result.isSuccess(result)).toBe(true);

    if (Result.isSuccess(result)) {
      const state = result.success;
      expect(state.mode).toBe("native");
      expect(state.supervised).toBe(true);
      expect(state.updated_at).toBe("2025-01-15T12:00:00Z");
      expect(state.health_timeout_seconds).toBe(150);

      expect(state.services["neuratrade-server"].status).toBe("running");
      expect(state.services["neuratrade-server"].detail).toBe("healthy");
      expect(state.services["neuratrade-server"].endpoint).toBe(
        "http://localhost:8080",
      );

      expect(state.services["telegram-service"].status).toBe("running");
      expect(state.services["telegram-service"].detail).toBeUndefined();
      expect(state.services["telegram-service"].endpoint).toBeUndefined();

      expect(state.services["ccxt-service"].status).toBe("stopped");
      expect(state.services["ccxt-service"].detail).toBe(
        "skipped: native mode",
      );
    }
  });

  test("decodes state with empty services map", () => {
    const input = {
      mode: "external",
      supervised: false,
      updated_at: "2025-01-15T12:00:00Z",
      health_timeout_seconds: 150,
      services: {},
    };

    const result = decodeGatewayStateEither(input);
    expect(Result.isSuccess(result)).toBe(true);

    if (Result.isSuccess(result)) {
      expect(result.success.services).toEqual({});
    }
  });

  // --- Invalid JSON ---

  test("rejects missing required fields", () => {
    const result = decodeGatewayStateEither({ mode: "native" });
    expect(Result.isFailure(result)).toBe(true);
  });

  test("rejects wrong type for mode", () => {
    const result = decodeGatewayStateEither({
      mode: 123,
      supervised: true,
      updated_at: "2025-01-15T12:00:00Z",
      health_timeout_seconds: 150,
      services: {},
    });
    expect(Result.isFailure(result)).toBe(true);
  });

  test("rejects wrong type for supervised", () => {
    const result = decodeGatewayStateEither({
      mode: "native",
      supervised: "yes",
      updated_at: "2025-01-15T12:00:00Z",
      health_timeout_seconds: 150,
      services: {},
    });
    expect(Result.isFailure(result)).toBe(true);
  });

  test("rejects wrong type for services", () => {
    const result = decodeGatewayStateEither({
      mode: "native",
      supervised: false,
      updated_at: "2025-01-15T12:00:00Z",
      health_timeout_seconds: 150,
      services: "not-a-map",
    });
    expect(Result.isFailure(result)).toBe(true);
  });

  test("rejects an array", () => {
    const result = decodeGatewayStateEither([]);
    expect(Result.isFailure(result)).toBe(true);
  });

  test("rejects null", () => {
    const result = decodeGatewayStateEither(null);
    expect(Result.isFailure(result)).toBe(true);
  });

  // --- Type compatibility ---

  test("GatewayState type is assignable", () => {
    const state: GatewayState = {
      mode: "native",
      supervised: false,
      updated_at: "2025-01-15T12:00:00Z",
      health_timeout_seconds: 150,
      services: {},
    };
    expect(state).toBeDefined();
  });

  test("GatewayServiceRuntime type is assignable", () => {
    const svc: GatewayServiceRuntime = {
      status: "running",
    };
    expect(svc.status).toBe("running");

    const svcFull: GatewayServiceRuntime = {
      status: "running",
      detail: "ok",
      endpoint: "http://localhost:8080",
    };
    expect(svcFull.detail).toBe("ok");
  });
});
