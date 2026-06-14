import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import * as fs from "fs";
import * as os from "os";
import * as nodePath from "path";
import { Effect, Layer } from "effect";
import { BunFileSystem } from "@effect/platform-bun";
import { PathLive } from "./path.ts";
import { PidFile, PidFileLive } from "./pid.ts";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Create a temp dir and return its path. */
function tmpDir(): string {
  return fs.mkdtempSync(nodePath.join(os.tmpdir(), "pid-test-"));
}

/** Remove a directory recursively, ignoring errors. */
function rmDir(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
    // ignore
  }
}

/** Build the test layer stack: Path + FileSystem + PidFile. */
function testLayer(homeDir: string): Layer.Layer<PidFile> {
  const baseLayer = Layer.merge(PathLive(homeDir), BunFileSystem.layer);
  return Layer.provide(PidFileLive, baseLayer);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("PidFile service", () => {
  let home: string;
  let pidsDir: string;

  beforeEach(() => {
    home = tmpDir();
    pidsDir = nodePath.join(home, "pids");
    fs.mkdirSync(pidsDir, { recursive: true });
  });

  afterEach(() => {
    rmDir(home);
  });

  // -----------------------------------------------------------------------
  // read / write round-trip
  // -----------------------------------------------------------------------

  describe("read / write round-trip", () => {
    it("writes a PID and reads it back", async () => {
      const program = Effect.gen(function* () {
        const pid = yield* PidFile;
        yield* pid.write("backend", 12345);
        return yield* pid.read("backend");
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBe(12345);
    });

    it("overwrites an existing PID file", async () => {
      const program = Effect.gen(function* () {
        const pid = yield* PidFile;
        yield* pid.write("backend", 100);
        yield* pid.write("backend", 200);
        return yield* pid.read("backend");
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBe(200);
    });

    it("returns null for a missing PID file", async () => {
      const program = Effect.gen(function* () {
        const pid = yield* PidFile;
        return yield* pid.read("nonexistent");
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBeNull();
    });

    it("reads a PID file written externally", async () => {
      // Write PID file directly via fs (simulating external writer)
      fs.writeFileSync(nodePath.join(pidsDir, "backend.pid"), "99999\n");

      const program = Effect.gen(function* () {
        const pid = yield* PidFile;
        return yield* pid.read("backend");
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBe(99999);
    });

    it("returns null when PID file contains invalid content", async () => {
      fs.writeFileSync(nodePath.join(pidsDir, "backend.pid"), "not-a-number");

      const program = Effect.gen(function* () {
        const pid = yield* PidFile;
        return yield* pid.read("backend");
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBeNull();
    });
  });

  // -----------------------------------------------------------------------
  // remove
  // -----------------------------------------------------------------------

  describe("remove", () => {
    it("removes an existing PID file", async () => {
      const program = Effect.gen(function* () {
        const pid = yield* PidFile;
        yield* pid.write("backend", 12345);
        yield* pid.remove("backend");
        return yield* pid.read("backend");
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBeNull();
    });

    it("does not throw when PID file does not exist", async () => {
      const program = Effect.gen(function* () {
        const pid = yield* PidFile;
        yield* pid.remove("nonexistent");
        return true;
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBe(true);
    });
  });

  // -----------------------------------------------------------------------
  // isRunning
  // -----------------------------------------------------------------------

  describe("isRunning", () => {
    it("returns true for a running process", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const pid = proc.pid;

      try {
        const program = Effect.gen(function* () {
          const pidFile = yield* PidFile;
          return yield* pidFile.isRunning(pid);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(testLayer(home))),
        );
        expect(result).toBe(true);
      } finally {
        proc.kill("SIGKILL");
        await proc.exited;
      }
    });

    it("returns false for a dead process", async () => {
      // Spawn and immediately kill to get a guaranteed-dead PID
      const proc = Bun.spawn(["sleep", "60"]);
      const pid = proc.pid;
      proc.kill("SIGKILL");
      await proc.exited;

      const program = Effect.gen(function* () {
        const pidFile = yield* PidFile;
        return yield* pidFile.isRunning(pid);
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBe(false);
    });

    it("returns false for a non-existent PID", async () => {
      const program = Effect.gen(function* () {
        const pidFile = yield* PidFile;
        return yield* pidFile.isRunning(2_147_483_647);
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBe(false);
    });
  });

  // -----------------------------------------------------------------------
  // processMatchesPattern
  // -----------------------------------------------------------------------

  describe("processMatchesPattern", () => {
    it("returns true when command matches a pattern", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const pid = proc.pid;

      try {
        const program = Effect.gen(function* () {
          const pidFile = yield* PidFile;
          return yield* pidFile.processMatchesPattern(pid, ["sleep"]);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(testLayer(home))),
        );
        expect(result).toBe(true);
      } finally {
        proc.kill("SIGKILL");
        await proc.exited;
      }
    });

    it("returns true with case-insensitive matching", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const pid = proc.pid;

      try {
        const program = Effect.gen(function* () {
          const pidFile = yield* PidFile;
          return yield* pidFile.processMatchesPattern(pid, ["SLEEP"]);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(testLayer(home))),
        );
        expect(result).toBe(true);
      } finally {
        proc.kill("SIGKILL");
        await proc.exited;
      }
    });

    it("returns false when command does not match any pattern", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const pid = proc.pid;

      try {
        const program = Effect.gen(function* () {
          const pidFile = yield* PidFile;
          return yield* pidFile.processMatchesPattern(pid, [
            "neuratrade-server",
            "telegram-service",
          ]);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(testLayer(home))),
        );
        expect(result).toBe(false);
      } finally {
        proc.kill("SIGKILL");
        await proc.exited;
      }
    });

    it("returns true when any of multiple patterns matches", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const pid = proc.pid;

      try {
        const program = Effect.gen(function* () {
          const pidFile = yield* PidFile;
          return yield* pidFile.processMatchesPattern(pid, [
            "neuratrade-server",
            "sleep",
          ]);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(testLayer(home))),
        );
        expect(result).toBe(true);
      } finally {
        proc.kill("SIGKILL");
        await proc.exited;
      }
    });
  });

  // -----------------------------------------------------------------------
  // cleanupStale
  // -----------------------------------------------------------------------

  describe("cleanupStale", () => {
    it("removes PID files for dead processes", async () => {
      // Spawn and kill to get a dead PID
      const proc = Bun.spawn(["sleep", "60"]);
      const deadPid = proc.pid;
      proc.kill("SIGKILL");
      await proc.exited;

      // Write the dead PID to a file
      fs.writeFileSync(nodePath.join(pidsDir, "backend.pid"), String(deadPid));

      const program = Effect.gen(function* () {
        const pidFile = yield* PidFile;
        yield* pidFile.cleanupStale(["backend"]);
        return yield* pidFile.read("backend");
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBeNull();
    });

    it("keeps PID files for running processes", async () => {
      const proc = Bun.spawn(["sleep", "60"]);
      const livePid = proc.pid;

      try {
        // Write the live PID to a file
        fs.writeFileSync(
          nodePath.join(pidsDir, "backend.pid"),
          String(livePid),
        );

        const program = Effect.gen(function* () {
          const pidFile = yield* PidFile;
          yield* pidFile.cleanupStale(["backend"]);
          return yield* pidFile.read("backend");
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(testLayer(home))),
        );
        expect(result).toBe(livePid);
      } finally {
        proc.kill("SIGKILL");
        await proc.exited;
      }
    });

    it("handles missing PID files gracefully", async () => {
      const program = Effect.gen(function* () {
        const pidFile = yield* PidFile;
        // Should not throw for a service with no PID file
        yield* pidFile.cleanupStale(["nonexistent"]);
        return true;
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(testLayer(home))),
      );
      expect(result).toBe(true);
    });

    it("handles multiple services in one call", async () => {
      // Dead PID for backend
      const deadProc = Bun.spawn(["sleep", "60"]);
      const deadPid = deadProc.pid;
      deadProc.kill("SIGKILL");
      await deadProc.exited;

      // Live PID for telegram
      const liveProc = Bun.spawn(["sleep", "60"]);
      const livePid = liveProc.pid;

      try {
        fs.writeFileSync(
          nodePath.join(pidsDir, "backend.pid"),
          String(deadPid),
        );
        fs.writeFileSync(
          nodePath.join(pidsDir, "telegram.pid"),
          String(livePid),
        );

        const program = Effect.gen(function* () {
          const pidFile = yield* PidFile;
          yield* pidFile.cleanupStale(["backend", "telegram"]);
          const backendPid = yield* pidFile.read("backend");
          const telegramPid = yield* pidFile.read("telegram");
          return { backendPid, telegramPid };
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(testLayer(home))),
        );
        expect(result.backendPid).toBeNull();
        expect(result.telegramPid).toBe(livePid);
      } finally {
        liveProc.kill("SIGKILL");
        await liveProc.exited;
      }
    });
  });
});
