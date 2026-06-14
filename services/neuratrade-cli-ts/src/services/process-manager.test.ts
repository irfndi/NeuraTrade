import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import * as fs from "fs";
import * as os from "os";
import * as nodePath from "path";
import { Effect, Layer } from "effect";
import { BunFileSystem } from "@effect/platform-bun";
import { PathLive } from "./path.ts";
import { PidFileLive } from "./pid.ts";
import { LoggerLive } from "./logger.ts";
import {
  ProcessManager,
  ProcessManagerLive,
  ProcessError,
  ALLOWED_BINARIES_DEFAULT,
} from "./process-manager.ts";

function tmpDir(): string {
  return fs.mkdtempSync(nodePath.join(os.tmpdir(), "pm-test-"));
}

function rmDir(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
    // ignore
  }
}

function testLayer(homeDir: string): Layer.Layer<ProcessManager> {
  const baseLayer = Layer.mergeAll(
    PathLive(homeDir),
    BunFileSystem.layer,
    LoggerLive,
  );
  const pidFileLayer = Layer.provide(PidFileLive, baseLayer);
  const fullLayer = Layer.merge(baseLayer, pidFileLayer);
  return Layer.provide(ProcessManagerLive, fullLayer);
}

async function run<A>(
  effect: Effect.Effect<A, unknown, ProcessManager>,
  home: string,
): Promise<A> {
  return Effect.runPromise(effect.pipe(Effect.provide(testLayer(home))));
}

async function expectFail(
  effect: Effect.Effect<unknown, ProcessError, ProcessManager>,
  home: string,
  matcher: (msg: string) => void,
): Promise<void> {
  try {
    await Effect.runPromise(effect.pipe(Effect.provide(testLayer(home))));
    throw new Error("Expected Effect to fail but it succeeded");
  } catch (err) {
    if (
      err instanceof Error &&
      err.message === "Expected Effect to fail but it succeeded"
    ) {
      throw err;
    }
    const msg = err instanceof Error ? err.message : String(err);
    matcher(msg);
  }
}

function whichSync(name: string): string | null {
  try {
    const result = Bun.spawnSync(["which", name]);
    if (result.exitCode === 0) {
      return result.stdout.toString().trim();
    }
  } catch {
    // ignore
  }
  return null;
}

// ===========================================================================
// Tests
// ===========================================================================

describe("ProcessManager service", () => {
  let home: string;
  let pidsDir: string;
  let logsDir: string;
  let originalAllowedBinaries: string | undefined;

  beforeEach(() => {
    home = tmpDir();
    pidsDir = nodePath.join(home, "pids");
    logsDir = nodePath.join(home, "logs");
    fs.mkdirSync(pidsDir, { recursive: true });
    fs.mkdirSync(logsDir, { recursive: true });
    originalAllowedBinaries = process.env.GATEWAY_ALLOWED_BINARIES;
    process.env.GATEWAY_ALLOWED_BINARIES = "sleep";
  });

  afterEach(() => {
    if (originalAllowedBinaries !== undefined) {
      process.env.GATEWAY_ALLOWED_BINARIES = originalAllowedBinaries;
    } else {
      delete process.env.GATEWAY_ALLOWED_BINARIES;
    }
    rmDir(home);
  });

  // -----------------------------------------------------------------------
  // resolveServiceBinary
  // -----------------------------------------------------------------------

  describe("resolveServiceBinary", () => {
    it("resolves an allowlisted binary from PATH", async () => {
      const result = await run(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.resolveServiceBinary("sleep");
        }),
        home,
      );
      expect(result).toBeTypeOf("string");
      expect(result.length).toBeGreaterThan(0);
      expect(result).toContain("sleep");
    });

    it("rejects a non-allowlisted binary", async () => {
      await expectFail(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.resolveServiceBinary("malicious-binary");
        }),
        home,
        (msg) => expect(msg).toContain("not allowlisted"),
      );
    });

    it("rejects an empty binary name", async () => {
      await expectFail(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.resolveServiceBinary("");
        }),
        home,
        (msg) => expect(msg).toContain("empty"),
      );
    });

    it("rejects a binary not found in PATH", async () => {
      await expectFail(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.resolveServiceBinary("sleep-not-a-real-binary-xyz");
        }),
        home,
        (msg) => expect(msg).toContain("not allowlisted"),
      );
    });

    it("resolves binary from execDir when provided", async () => {
      const execDir = nodePath.join(home, "bin");
      fs.mkdirSync(execDir, { recursive: true });
      const realSleep = whichSync("sleep");
      if (realSleep) {
        const linkPath = nodePath.join(execDir, "my-service");
        fs.copyFileSync(realSleep, linkPath);
        fs.chmodSync(linkPath, 0o755);

        process.env.GATEWAY_ALLOWED_BINARIES = "my-service";

        const result = await run(
          Effect.gen(function* () {
            const pm = yield* ProcessManager;
            return yield* pm.resolveServiceBinary("my-service", execDir);
          }),
          home,
        );
        expect(result).toContain("my-service");
      }
    });
  });

  // -----------------------------------------------------------------------
  // startService + PID round-trip
  // -----------------------------------------------------------------------

  describe("startService", () => {
    it("spawns a process and writes a PID file", async () => {
      const logPath = nodePath.join(logsDir, "test-service.log");

      const result = await run(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          const proc = yield* pm.startService(
            "sleep",
            "test-service",
            logPath,
            {},
            "test-service",
          );
          const pid = proc.pid;
          yield* Effect.sleep("50 millis");
          return pid;
        }),
        home,
      );

      expect(result).toBeGreaterThan(0);

      const pidFilePath = nodePath.join(pidsDir, "test-service.pid");
      expect(fs.existsSync(pidFilePath)).toBe(true);
      const content = fs.readFileSync(pidFilePath, "utf-8").trim();
      expect(Number(content)).toBe(result);

      expect(fs.existsSync(logPath)).toBe(true);

      try {
        process.kill(result, "SIGKILL");
      } catch {
        // already dead
      }
    });

    it("spawns a process with custom env vars", async () => {
      const logPath = nodePath.join(logsDir, "env-test.log");

      const result = await run(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          const proc = yield* pm.startService(
            "sleep",
            "env-test",
            logPath,
            { MY_VAR: "hello" },
            "env-test",
          );
          return proc.pid;
        }),
        home,
      );

      expect(result).toBeGreaterThan(0);

      try {
        process.kill(result, "SIGKILL");
      } catch {
        // already dead
      }
    });

    it("returns an error for a non-allowlisted binary", async () => {
      const logPath = nodePath.join(logsDir, "bad.log");

      await expectFail(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.startService(
            "malicious-binary",
            "bad",
            logPath,
            {},
            "bad",
          );
        }),
        home,
        (msg) => expect(msg).toContain("not allowlisted"),
      );
    });

    it("does not crash when pidFile is empty string", async () => {
      const logPath = nodePath.join(logsDir, "no-pid.log");

      const result = await run(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          const proc = yield* pm.startService(
            "sleep",
            "no-pid",
            logPath,
            {},
            "",
          );
          return proc.pid;
        }),
        home,
      );

      expect(result).toBeGreaterThan(0);

      const pidFiles = fs
        .readdirSync(pidsDir)
        .filter((f) => f.endsWith(".pid"));
      expect(pidFiles.length).toBe(0);

      try {
        process.kill(result, "SIGKILL");
      } catch {
        // already dead
      }
    });
  });

  // -----------------------------------------------------------------------
  // stopServiceByPIDFile
  // -----------------------------------------------------------------------

  describe("stopServiceByPIDFile", () => {
    it("stops a running process via PID file", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const pid = proc.pid;

      fs.writeFileSync(nodePath.join(pidsDir, "stop-test.pid"), String(pid));

      await run(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          yield* pm.stopServiceByPIDFile("stop-test", "stop-test", ["sleep"]);
        }),
        home,
      );

      let alive = false;
      try {
        process.kill(pid, 0);
        alive = true;
      } catch {
        alive = false;
      }
      expect(alive).toBe(false);

      expect(fs.existsSync(nodePath.join(pidsDir, "stop-test.pid"))).toBe(
        false,
      );
    });

    it("returns error when PID file does not exist", async () => {
      await expectFail(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.stopServiceByPIDFile(
            "nonexistent",
            "nonexistent",
            [],
          );
        }),
        home,
        (msg) => expect(msg).toContain("not running"),
      );
    });

    it("removes stale PID file when pattern does not match", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const pid = proc.pid;

      fs.writeFileSync(nodePath.join(pidsDir, "stale-test.pid"), String(pid));

      await expectFail(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.stopServiceByPIDFile("stale-test", "stale-test", [
            "neuratrade-server",
          ]);
        }),
        home,
        (msg) => expect(msg).toContain("stale"),
      );

      expect(fs.existsSync(nodePath.join(pidsDir, "stale-test.pid"))).toBe(
        false,
      );

      let alive = false;
      try {
        process.kill(pid, 0);
        alive = true;
      } catch {
        alive = false;
      }
      expect(alive).toBe(true);

      try {
        proc.kill("SIGKILL");
        await proc.exited;
      } catch {
        // ignore
      }
    });

    it("removes PID file when process is already dead", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const deadPid = proc.pid;
      proc.kill("SIGKILL");
      await proc.exited;

      fs.writeFileSync(
        nodePath.join(pidsDir, "dead-test.pid"),
        String(deadPid),
      );

      await expectFail(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.stopServiceByPIDFile("dead-test", "dead-test", []);
        }),
        home,
        (msg) => expect(msg).toContain("not found"),
      );

      expect(fs.existsSync(nodePath.join(pidsDir, "dead-test.pid"))).toBe(
        false,
      );
    });
  });

  // -----------------------------------------------------------------------
  // signalAndWait
  // -----------------------------------------------------------------------

  describe("signalAndWait", () => {
    it("sends signal and returns true when process exits", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const pid = proc.pid;

      await Bun.sleep(50);

      const result = await run(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.signalAndWait(proc, "SIGTERM", 5000);
        }),
        home,
      );

      expect(result).toBe(true);

      let alive = false;
      try {
        process.kill(pid, 0);
        alive = true;
      } catch {
        alive = false;
      }
      expect(alive).toBe(false);
    });

    it("returns true for already-dead process", async () => {
      const proc = Bun.spawn(["sleep", "0.01"]);
      await proc.exited;

      const result = await run(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          return yield* pm.signalAndWait(proc, "SIGTERM", 1000);
        }),
        home,
      );

      expect(result).toBe(true);
    });
  });

  // -----------------------------------------------------------------------
  // cleanupStalePIDs
  // -----------------------------------------------------------------------

  describe("cleanupStalePIDs", () => {
    it("removes PID files for dead processes", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const deadPid = proc.pid;
      proc.kill("SIGKILL");
      await proc.exited;

      fs.writeFileSync(
        nodePath.join(pidsDir, "cleanup-dead.pid"),
        String(deadPid),
      );

      await run(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          yield* pm.cleanupStalePIDs(["cleanup-dead"]);
        }),
        home,
      );

      expect(fs.existsSync(nodePath.join(pidsDir, "cleanup-dead.pid"))).toBe(
        false,
      );
    });

    it("keeps PID files for running processes", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const livePid = proc.pid;

      try {
        fs.writeFileSync(
          nodePath.join(pidsDir, "cleanup-live.pid"),
          String(livePid),
        );

        await run(
          Effect.gen(function* () {
            const pm = yield* ProcessManager;
            yield* pm.cleanupStalePIDs(["cleanup-live"]);
          }),
          home,
        );

        expect(fs.existsSync(nodePath.join(pidsDir, "cleanup-live.pid"))).toBe(
          true,
        );

        const content = fs
          .readFileSync(nodePath.join(pidsDir, "cleanup-live.pid"), "utf-8")
          .trim();
        expect(Number(content)).toBe(livePid);
      } finally {
        proc.kill("SIGKILL");
        await proc.exited;
      }
    });

    it("handles missing PID files gracefully", async () => {
      await run(
        Effect.gen(function* () {
          const pm = yield* ProcessManager;
          yield* pm.cleanupStalePIDs(["nonexistent"]);
        }),
        home,
      );
    });

    it("handles multiple services in one call", async () => {
      const deadProc = Bun.spawn(["sleep", "60"]);
      const deadPid = deadProc.pid;
      deadProc.kill("SIGKILL");
      await deadProc.exited;

      const liveProc = Bun.spawn(["sleep", "60"]);
      const livePid = liveProc.pid;

      try {
        fs.writeFileSync(
          nodePath.join(pidsDir, "multi-dead.pid"),
          String(deadPid),
        );
        fs.writeFileSync(
          nodePath.join(pidsDir, "multi-live.pid"),
          String(livePid),
        );

        await run(
          Effect.gen(function* () {
            const pm = yield* ProcessManager;
            yield* pm.cleanupStalePIDs(["multi-dead", "multi-live"]);
          }),
          home,
        );

        expect(fs.existsSync(nodePath.join(pidsDir, "multi-dead.pid"))).toBe(
          false,
        );
        expect(fs.existsSync(nodePath.join(pidsDir, "multi-live.pid"))).toBe(
          true,
        );
      } finally {
        liveProc.kill("SIGKILL");
        await liveProc.exited;
      }
    });
  });

  // -----------------------------------------------------------------------
  // ALLOWED_BINARIES_DEFAULT
  // -----------------------------------------------------------------------

  describe("ALLOWED_BINARIES_DEFAULT", () => {
    it("contains the three expected service names", () => {
      expect(ALLOWED_BINARIES_DEFAULT.has("neuratrade-server")).toBe(true);
      expect(ALLOWED_BINARIES_DEFAULT.has("telegram-service")).toBe(true);
      expect(ALLOWED_BINARIES_DEFAULT.has("ccxt-service")).toBe(true);
      expect(ALLOWED_BINARIES_DEFAULT.size).toBe(3);
    });
  });
});
