import { Context, Effect, Layer } from "effect";
import { FileSystem } from "@effect/platform";
import { Path } from "./path.ts";

/**
 * The PidFile service interface. Reads, writes, and manages PID files
 * under `$NEURATRADE_HOME/pids/`. Mirrors the Go gateway CLI PID
 * helpers (`readPIDFile`, `writePIDFile`, `processRunning`,
 * `processMatchesAnyPattern`, `removePIDFileIfProcessExited`,
 * `cleanupStalePIDs`).
 */
export class PidFile extends Context.Tag("PidFile")<
  PidFile,
  {
    /** Read the PID stored for `service`, or `null` if missing/invalid. */
    readonly read: (service: string) => Effect.Effect<number | null, never>;

    /** Write `pid` to the PID file for `service`. */
    readonly write: (
      service: string,
      pid: number,
    ) => Effect.Effect<void, never>;

    /** Remove the PID file for `service` (no-op if absent). */
    readonly remove: (service: string) => Effect.Effect<void, never>;

    /** Check whether a process with the given PID is alive (signal 0 probe). */
    readonly isRunning: (pid: number) => Effect.Effect<boolean, never>;

    /**
     * Run `ps -p <pid> -o command=` and check whether the output contains
     * any of the given patterns (case-insensitive).
     */
    readonly processMatchesPattern: (
      pid: number,
      patterns: ReadonlyArray<string>,
    ) => Effect.Effect<boolean, never>;

    /**
     * For each service name, read its PID file. If the PID is stale
     * (process no longer alive), remove the file. Running processes
     * are left untouched.
     */
    readonly cleanupStale: (
      services: ReadonlyArray<string>,
    ) => Effect.Effect<void, never>;
  }
>() {}

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

/** Check whether a Unix process is alive via kill(pid, 0). */
function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

/**
 * Run `ps -p <pid> -o command=` and return the trimmed, lowercased output.
 * Returns an empty string if the command fails (process gone, permission, etc.).
 */
function psCommand(pid: number): Effect.Effect<string, never> {
  return Effect.gen(function* () {
    const proc = Bun.spawn(["ps", "-p", String(pid), "-o", "command="], {
      stdout: "pipe",
      stderr: "pipe",
    });
    const exitCode = yield* Effect.promise(() => proc.exited);
    if (exitCode !== 0) return "";
    const output = yield* Effect.promise(() =>
      new Response(proc.stdout).text(),
    );
    return output.trim().toLowerCase();
  }).pipe(Effect.catchAll(() => Effect.succeed("")));
}

/** Live PidFile layer backed by the real filesystem and process signals. */
export const PidFileLive: Layer.Layer<
  PidFile,
  never,
  Path | FileSystem.FileSystem
> = Layer.effect(
  PidFile,
  Effect.gen(function* () {
    const path = yield* Path;
    const fs = yield* FileSystem.FileSystem;

    /** Resolve the PID file path for a service. */
    const pidPath = (service: string): string => path.pidFilePath(service);

    const read = (service: string): Effect.Effect<number | null, never> =>
      Effect.gen(function* () {
        const filePath = pidPath(service);
        const exists = yield* fs.exists(filePath);
        if (!exists) return null;

        const content = yield* fs.readFileString(filePath);
        const trimmed = content.trim();
        const parsed = Number(trimmed);
        if (
          !Number.isFinite(parsed) ||
          parsed <= 0 ||
          !Number.isInteger(parsed)
        ) {
          return null;
        }
        return parsed;
      }).pipe(Effect.catchAll(() => Effect.succeed(null)));

    const write = (service: string, pid: number): Effect.Effect<void, never> =>
      Effect.gen(function* () {
        const filePath = pidPath(service);
        yield* fs.writeFileString(filePath, String(pid));
      }).pipe(Effect.catchAll(() => Effect.void));

    const remove = (service: string): Effect.Effect<void, never> =>
      Effect.gen(function* () {
        const filePath = pidPath(service);
        const exists = yield* fs.exists(filePath);
        if (exists) {
          yield* fs.remove(filePath);
        }
      }).pipe(Effect.catchAll(() => Effect.void));

    const isRunning = (pid: number): Effect.Effect<boolean, never> =>
      Effect.sync(() => isProcessAlive(pid));

    const processMatchesPattern = (
      pid: number,
      patterns: ReadonlyArray<string>,
    ): Effect.Effect<boolean, never> =>
      Effect.gen(function* () {
        const command = yield* psCommand(pid);
        for (const pattern of patterns) {
          if (command.includes(pattern.toLowerCase())) {
            return true;
          }
        }
        return false;
      });

    const cleanupStale = (
      services: ReadonlyArray<string>,
    ): Effect.Effect<void, never> =>
      Effect.gen(function* () {
        for (const service of services) {
          const pid = yield* read(service);
          if (pid === null) continue;

          const alive = yield* isRunning(pid);
          if (!alive) {
            yield* remove(service);
          }
        }
      });

    return {
      read,
      write,
      remove,
      isRunning,
      processMatchesPattern,
      cleanupStale,
    };
  }),
);
