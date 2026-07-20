/**
 * GatewayState service — reads/writes the gateway runtime state JSON file
 * (`pids/gateway-state.json`).
 *
 * Mirrors the Go functions in cmd/neuratrade-cli/gateway.go:
 *   writeGatewayState, readGatewayState, writeGatewayStateMode,
 *   writeGatewayServiceState, markGatewayStopped.
 */
import { Context, Effect, Layer } from "effect";
import { FileSystem } from "effect";
import { Path } from "./path.ts";
import type { GatewayState as GatewayStateData } from "../schemas/gateway-state.ts";
import { decodeGatewayState } from "../schemas/gateway-state.ts";

// ---------------------------------------------------------------------------
// Mutable state type (Schema types are readonly)
// ---------------------------------------------------------------------------

interface MutableGatewayServiceState {
  status: string;
  detail?: string;
  endpoint?: string;
}

interface MutableGatewayState {
  mode: string;
  supervised: boolean;
  updated_at: string;
  health_timeout_seconds: number;
  services: Record<string, MutableGatewayServiceState>;
}

// ---------------------------------------------------------------------------
// Default state (matches Go readGatewayState zero-value)
// ---------------------------------------------------------------------------

function defaultGatewayState(): MutableGatewayState {
  return {
    mode: "",
    supervised: false,
    updated_at: "",
    health_timeout_seconds: 0,
    services: {},
  };
}

// ---------------------------------------------------------------------------
// Context.Tag
// ---------------------------------------------------------------------------

export interface GatewayStateService {
  /** Read the persisted gateway state. Returns a default state if the file is missing or corrupt. */
  readonly read: () => Effect.Effect<GatewayStateData, never>;
  /** Write the full gateway state to disk. Sets `updated_at` to the current ISO timestamp and creates parent directories if needed. */
  readonly write: (state: GatewayStateData) => Effect.Effect<void, never>;
  /** Update the gateway mode (and optionally attach a gateway service detail). Reads the existing state first. */
  readonly writeMode: (
    mode: string,
    detail?: string,
  ) => Effect.Effect<void, never>;
  /** Update a single service entry. Reads the existing state first. */
  readonly writeServiceState: (
    service: string,
    status: string,
    detail?: string,
    endpoint?: string,
  ) => Effect.Effect<void, never>;
  /** Mark the gateway and all known services as down. */
  readonly markStopped: (detail?: string) => Effect.Effect<void, never>;
}

export const GatewayState =
  Context.Service<GatewayStateService>("GatewayState");

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function readRawJson(
  fs: FileSystem.FileSystem,
  filePath: string,
): Effect.Effect<unknown, never> {
  return Effect.gen(function* () {
    const exists = yield* fs.exists(filePath);
    if (!exists) return null;
    const content = yield* fs.readFileString(filePath);
    try {
      return JSON.parse(content) as unknown;
    } catch {
      return null;
    }
  }).pipe(Effect.catch(() => Effect.succeed(null)));
}

/** Decode raw JSON into a mutable GatewayState, falling back to defaults. */
function decodeOrFallback(
  raw: unknown,
): Effect.Effect<MutableGatewayState, never> {
  if (raw === null || raw === undefined)
    return Effect.succeed(defaultGatewayState());
  return decodeGatewayState(raw).pipe(
    Effect.map((decoded) => ({
      mode: decoded.mode,
      supervised: decoded.supervised,
      updated_at: decoded.updated_at,
      health_timeout_seconds: decoded.health_timeout_seconds,
      services: decoded.services ? { ...decoded.services } : {},
    })),
    Effect.catch(() => Effect.succeed(defaultGatewayState())),
  );
}

/** Stamp `updated_at` and JSON-serialize. */
function serializeState(state: MutableGatewayState): string {
  state.updated_at = new Date().toISOString();
  return JSON.stringify(state, null, 2);
}

// ---------------------------------------------------------------------------
// Known service names (matching Go markGatewayStopped)
// ---------------------------------------------------------------------------

const KNOWN_SERVICES = ["gateway", "backend", "ccxt", "telegram"];

// ---------------------------------------------------------------------------
// Layer
// ---------------------------------------------------------------------------

/**
 * Live layer for the GatewayState service.
 *
 * Requires `Path` and `FileSystem` in context.
 */
export const GatewayStateLive: Layer.Layer<
  GatewayStateService,
  never,
  Path | FileSystem.FileSystem
> = Layer.effect(
  GatewayState,
  Effect.gen(function* () {
    const path = yield* Path;
    const fs = yield* FileSystem.FileSystem;
    const filePath = path.gatewayStatePath;

    const read = (): Effect.Effect<GatewayStateData, never> =>
      Effect.gen(function* () {
        const raw = yield* readRawJson(fs, filePath);
        return yield* decodeOrFallback(raw);
      });

    const write = (state: GatewayStateData): Effect.Effect<void, never> =>
      Effect.gen(function* () {
        const mutable: MutableGatewayState = {
          mode: state.mode,
          supervised: state.supervised,
          updated_at: state.updated_at,
          health_timeout_seconds: state.health_timeout_seconds,
          services: state.services ? { ...state.services } : {},
        };
        const serialized = serializeState(mutable);
        // Ensure parent directory exists
        const dir = filePath.substring(0, filePath.lastIndexOf("/"));
        yield* fs
          .makeDirectory(dir, { recursive: true })
          .pipe(Effect.catch(() => Effect.void));
        yield* fs
          .writeFileString(filePath, serialized)
          .pipe(Effect.catch(() => Effect.void));
      });

    const writeMode = (
      mode: string,
      detail?: string,
    ): Effect.Effect<void, never> =>
      Effect.gen(function* () {
        const state = yield* read();
        const mutable: MutableGatewayState = {
          mode,
          supervised: state.supervised,
          updated_at: state.updated_at,
          health_timeout_seconds: state.health_timeout_seconds,
          services: state.services ? { ...state.services } : {},
        };
        if (detail !== undefined && detail !== "") {
          mutable.services["gateway"] = { status: mode, detail };
        }
        yield* write(mutable);
      });

    const writeServiceState = (
      service: string,
      status: string,
      detail?: string,
      endpoint?: string,
    ): Effect.Effect<void, never> =>
      Effect.gen(function* () {
        const state = yield* read();
        const mutable: MutableGatewayState = {
          mode: state.mode,
          supervised: state.supervised,
          updated_at: state.updated_at,
          health_timeout_seconds: state.health_timeout_seconds,
          services: state.services ? { ...state.services } : {},
        };
        const entry: MutableGatewayServiceState = { status };
        if (detail !== undefined) entry.detail = detail;
        if (endpoint !== undefined) entry.endpoint = endpoint;
        mutable.services[service] = entry;
        yield* write(mutable);
      });

    const markStopped = (detail?: string): Effect.Effect<void, never> =>
      Effect.gen(function* () {
        const effectiveDetail = detail ?? "gateway stopped";
        const state = yield* read();
        const mutable: MutableGatewayState = {
          mode: "down",
          supervised: state.supervised,
          updated_at: state.updated_at,
          health_timeout_seconds: state.health_timeout_seconds,
          services: state.services ? { ...state.services } : {},
        };
        mutable.services["gateway"] = {
          status: "down",
          detail: effectiveDetail,
        };
        for (const name of KNOWN_SERVICES) {
          if (name === "gateway") continue;
          mutable.services[name] = { status: "down", detail: effectiveDetail };
        }
        yield* write(mutable);
      });

    return {
      read,
      write,
      writeMode,
      writeServiceState,
      markStopped,
    } as GatewayStateService;
  }),
);
