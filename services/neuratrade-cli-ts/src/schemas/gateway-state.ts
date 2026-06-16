/**
 * Schema definitions for gatewayRuntimeState (gateway-state.json).
 *
 * Mirrors the Go structs in cmd/neuratrade-cli/gateway.go:
 *   gatewayRuntimeState and gatewayServiceRuntime.
 */
import * as S from "effect/Schema";

// -- Sub-schema for a single service entry --

const GatewayServiceRuntimeSchema = S.Struct({
  status: S.String,
  detail: S.optional(S.String),
  endpoint: S.optional(S.String),
});

export type GatewayServiceRuntime = typeof GatewayServiceRuntimeSchema.Type;

// -- Main gatewayRuntimeState schema --

export const GatewayStateSchema = S.Struct({
  mode: S.String,
  supervised: S.Boolean,
  updated_at: S.String,
  health_timeout_seconds: S.Number,
  services: S.Record({ key: S.String, value: GatewayServiceRuntimeSchema }),
});

export type GatewayState = typeof GatewayStateSchema.Type;

/** Decode an unknown JSON value into GatewayState (returns Effect). */
export const decodeGatewayState = S.decodeUnknown(GatewayStateSchema);

/** Decode an unknown JSON value, returning Either. */
export const decodeGatewayStateEither =
  S.decodeUnknownEither(GatewayStateSchema);
