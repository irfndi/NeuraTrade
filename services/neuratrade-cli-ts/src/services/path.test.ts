import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import * as os from "os";
import * as path from "path";
import { Effect, Layer } from "effect";
import { Path, PathLive } from "./path.ts";

describe("Path service", () => {
  const originalEnv = { ...process.env };

  afterEach(() => {
    process.env = { ...originalEnv };
  });

  describe("PathLive with explicit homeDir", () => {
    it("uses the provided home directory", () => {
      const home = "/tmp/test-neuratrade-explicit";
      const layer = PathLive(home);
      const program = Effect.gen(function* () {
        const path = yield* Path;
        return path.homeDir;
      });
      const result = Effect.runSync(program.pipe(Effect.provide(layer)));
      expect(result).toBe(home);
    });

    it("resolves all sub-paths from the explicit home", () => {
      const home = "/tmp/test-neuratrade-explicit";
      const layer = PathLive(home);
      const program = Effect.gen(function* () {
        const p = yield* Path;
        return {
          pidDir: p.pidDir,
          logDir: p.logDir,
          dataDir: p.dataDir,
          configPath: p.configPath,
          runtimeConfigPath: p.runtimeConfigPath,
          gatewayStatePath: p.gatewayStatePath,
        };
      });
      const result = Effect.runSync(program.pipe(Effect.provide(layer)));
      expect(result).toEqual({
        pidDir: path.join(home, "pids"),
        logDir: path.join(home, "logs"),
        dataDir: path.join(home, "data"),
        configPath: path.join(home, "config.json"),
        runtimeConfigPath: path.join(home, "runtime.json"),
        gatewayStatePath: path.join(home, "pids", "gateway-state.json"),
      });
    });
  });

  describe("PathLive reads NEURATRADE_HOME env", () => {
    beforeEach(() => {
      process.env.NEURATRADE_HOME = "/tmp/test-neuratrade-env";
    });

    it("uses NEURATRADE_HOME when no homeDir provided", () => {
      const layer = PathLive();
      const program = Effect.gen(function* () {
        const p = yield* Path;
        return p.homeDir;
      });
      const result = Effect.runSync(program.pipe(Effect.provide(layer)));
      expect(result).toBe("/tmp/test-neuratrade-env");
    });

    it("env-sourced paths are correctly derived", () => {
      const layer = PathLive();
      const program = Effect.gen(function* () {
        const p = yield* Path;
        return {
          pidDir: p.pidDir,
          logDir: p.logDir,
          dataDir: p.dataDir,
          configPath: p.configPath,
          runtimeConfigPath: p.runtimeConfigPath,
          gatewayStatePath: p.gatewayStatePath,
        };
      });
      const result = Effect.runSync(program.pipe(Effect.provide(layer)));
      expect(result).toEqual({
        pidDir: "/tmp/test-neuratrade-env/pids",
        logDir: "/tmp/test-neuratrade-env/logs",
        dataDir: "/tmp/test-neuratrade-env/data",
        configPath: "/tmp/test-neuratrade-env/config.json",
        runtimeConfigPath: "/tmp/test-neuratrade-env/runtime.json",
        gatewayStatePath: "/tmp/test-neuratrade-env/pids/gateway-state.json",
      });
    });
  });

  describe("PathLive defaults to ~/.neuratrade", () => {
    beforeEach(() => {
      delete process.env.NEURATRADE_HOME;
    });

    it("defaults to ~/.neuratrade when env unset and no homeDir", () => {
      const layer = PathLive();
      const program = Effect.gen(function* () {
        const p = yield* Path;
        return p.homeDir;
      });
      const result = Effect.runSync(program.pipe(Effect.provide(layer)));
      expect(result).toBe(path.join(os.homedir(), ".neuratrade"));
    });
  });

  describe("tilde expansion", () => {
    it("expands ~ to the OS home directory", () => {
      const layer = PathLive("~/my-neuratrade");
      const program = Effect.gen(function* () {
        const p = yield* Path;
        return p.homeDir;
      });
      const result = Effect.runSync(program.pipe(Effect.provide(layer)));
      expect(result).toBe(path.join(os.homedir(), "my-neuratrade"));
    });

    it("does not expand paths without leading ~", () => {
      const layer = PathLive("/opt/neuratrade");
      const program = Effect.gen(function* () {
        const p = yield* Path;
        return p.homeDir;
      });
      const result = Effect.runSync(program.pipe(Effect.provide(layer)));
      expect(result).toBe("/opt/neuratrade");
    });
  });

  describe("pidFilePath", () => {
    it("returns the PID file path for a given service name", () => {
      const home = "/tmp/test-neuratrade-pid";
      const layer = PathLive(home);
      const program = Effect.gen(function* () {
        const p = yield* Path;
        return {
          backend: p.pidFilePath("backend"),
          ccxt: p.pidFilePath("ccxt"),
          telegram: p.pidFilePath("telegram"),
          gateway: p.pidFilePath("gateway"),
          agent: p.pidFilePath("agent"),
        };
      });
      const result = Effect.runSync(program.pipe(Effect.provide(layer)));
      expect(result).toEqual({
        backend: path.join(home, "pids", "backend.pid"),
        ccxt: path.join(home, "pids", "ccxt.pid"),
        telegram: path.join(home, "pids", "telegram.pid"),
        gateway: path.join(home, "pids", "gateway.pid"),
        agent: path.join(home, "pids", "agent.pid"),
      });
    });
  });

  describe("explicit homeDir takes precedence over env", () => {
    it("uses homeDir even when NEURATRADE_HOME is set", () => {
      process.env.NEURATRADE_HOME = "/tmp/test-neuratrade-ignored";
      const layer = PathLive("/tmp/test-neuratrade-wins");
      const program = Effect.gen(function* () {
        const p = yield* Path;
        return p.homeDir;
      });
      const result = Effect.runSync(program.pipe(Effect.provide(layer)));
      expect(result).toBe("/tmp/test-neuratrade-wins");
    });
  });
});
