import * as nodePath from "path";
import { Context, Effect, Layer } from "effect";
import { PidFile } from "./pid.ts";
import { Logger } from "./logger.ts";
import { signalAndWait as signalAndWaitUtil } from "../utils/signal.ts";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/**
 * Process error with a descriptive message.
 * Mirrors the Go pattern of wrapping errors with context.
 */
export class ProcessError extends Error {
  readonly _tag = "ProcessError";
  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
  }
}

// ---------------------------------------------------------------------------
// Default binary allowlist
// ---------------------------------------------------------------------------

/** The default set of allowed gateway service binary basenames. */
export const ALLOWED_BINARIES_DEFAULT: ReadonlySet<string> = new Set([
  "neuratrade-server",
  "telegram-service",
  "ccxt-service",
]);

/**
 * Resolve the set of allowed binary basenames.
 *
 * If `GATEWAY_ALLOWED_BINARIES` is set, its comma-separated values
 * (trimmed, basenames extracted) are used instead. Empty entries and
 * invalid basenames are ignored. If parsing yields no valid entries,
 * the default set is returned.
 *
 * Mirrors Go `newAllowedGatewayServiceBinaries()`.
 */
function resolveAllowedBinaries(): ReadonlySet<string> {
  const raw = process.env.GATEWAY_ALLOWED_BINARIES?.trim();
  if (!raw) return ALLOWED_BINARIES_DEFAULT;

  const custom = new Set<string>();
  for (const entry of raw.split(",")) {
    const trimmed = entry.trim();
    if (trimmed === "") continue;
    const base = nodePath.basename(trimmed);
    if (base === "" || base === "." || base === "..") continue;
    custom.add(base);
  }
  return custom.size > 0 ? custom : ALLOWED_BINARIES_DEFAULT;
}

// ---------------------------------------------------------------------------
// ProcessManager interface
// ---------------------------------------------------------------------------

/**
 * Process manager service interface.
 *
 * Spawns services via `Bun.spawn`, manages PID files via the `PidFile`
 * service, sends signals, and cleans up stale PIDs.
 *
 * Mirrors the Go gateway CLI process management functions:
 * `startService`, `stopServiceByPIDFile`, `signalAndWait`,
 * `cleanupStalePIDs`, `resolveServiceBinary`.
 */
export class ProcessManager extends Context.Tag("ProcessManager")<
  ProcessManager,
  {
    /**
     * Resolve a service binary by name. Checks the allowlist and uses
     * PATH lookup (plus optional `execDir`) to find the absolute path.
     *
     * Mirrors Go `resolveServiceBinary()`.
     */
    readonly resolveServiceBinary: (
      name: string,
      execDir?: string,
    ) => Effect.Effect<string, ProcessError>;

    /**
     * Spawn a service process, redirect stdout/stderr to `logPath`,
     * and write the PID file via the `PidFile` service.
     *
     * Mirrors Go `startService()`.
     */
    readonly startService: (
      binary: string,
      name: string,
      logPath: string,
      env: Record<string, string>,
      pidFile: string,
    ) => Effect.Effect<Bun.Subprocess, ProcessError>;

    /**
     * Stop a service by reading its PID file, validating the process
     * pattern, sending SIGTERM, waiting, sending SIGKILL if needed,
     * and removing the PID file.
     *
     * Mirrors Go `stopServiceByPIDFile()`.
     */
    readonly stopServiceByPIDFile: (
      name: string,
      pidFile: string,
      patterns: ReadonlyArray<string>,
    ) => Effect.Effect<void, ProcessError>;

    /**
     * Send a signal to a `Bun.Subprocess` and wait for it to exit
     * or for the timeout to elapse. Falls back to SIGKILL on timeout.
     *
     * Wraps the existing `signalAndWait` utility.
     */
    readonly signalAndWait: (
      subprocess: Bun.Subprocess,
      signal: NodeJS.Signals,
      timeoutMs: number,
    ) => Effect.Effect<boolean, never>;

    /**
     * For each service name, read its PID file. If the PID is stale
     * (process no longer alive), remove the file. Running processes
     * are left untouched.
     */
    readonly cleanupStalePIDs: (
      services: ReadonlyArray<string>,
    ) => Effect.Effect<void, never>;
  }
>() {}

// ---------------------------------------------------------------------------
// LookPath — find binary in PATH (or execDir)
// ---------------------------------------------------------------------------

/**
 * Find the absolute path of `name` in PATH directories (or `execDir`).
 * Returns `null` if not found. Uses `which` under the hood.
 *
 * Mirrors Go `exec.LookPath()`.
 */
function lookPath(name: string, execDir?: string): string | null {
  // Build the search PATH: execDir first, then system PATH
  const searchPath = execDir
    ? `${execDir}:${process.env.PATH ?? ""}`
    : (process.env.PATH ?? "");

  // Use `which` to find the binary
  try {
    const result = Bun.spawnSync(["which", name], {
      env: { ...process.env, PATH: searchPath },
      stdout: "pipe",
      stderr: "pipe",
    });
    if (result.exitCode === 0) {
      return result.stdout.toString().trim();
    }
  } catch {
    // which not found or other error
  }
  return null;
}

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

/** Live ProcessManager layer backed by real Bun.spawn and PidFile. */
export const ProcessManagerLive: Layer.Layer<
  ProcessManager,
  never,
  PidFile | Logger
> = Layer.effect(
  ProcessManager,
  Effect.gen(function* () {
    const pidFile = yield* PidFile;
    const logger = yield* Logger;

    const resolveServiceBinary = (
      name: string,
      execDir?: string,
    ): Effect.Effect<string, ProcessError> =>
      Effect.gen(function* () {
        const trimmed = name.trim();
        if (trimmed === "") {
          return yield* Effect.fail(new ProcessError("binary path is empty"));
        }

        const base = nodePath.basename(trimmed);
        const allowed = resolveAllowedBinaries();
        if (!allowed.has(base)) {
          return yield* Effect.fail(
            new ProcessError(
              `binary ${JSON.stringify(base)} is not allowlisted`,
            ),
          );
        }

        const resolved = lookPath(trimmed, execDir);
        if (resolved === null) {
          return yield* Effect.fail(
            new ProcessError(
              `look up binary ${JSON.stringify(trimmed)}: not found in PATH`,
            ),
          );
        }

        return resolved;
      });

    const startService = (
      binary: string,
      name: string,
      logPath: string,
      env: Record<string, string>,
      pidFileService: string,
    ): Effect.Effect<Bun.Subprocess, ProcessError> =>
      Effect.gen(function* () {
        // Resolve and validate binary
        const resolved = yield* resolveServiceBinary(binary);

        yield* logger.info(`Starting ${name}`, { binary: resolved });

        // Prepare environment variables
        const processEnv: Record<string, string> = {
          ...Object.fromEntries(
            Object.entries(process.env).filter(
              ([, v]) => v !== undefined,
            ) as Array<[string, string]>,
          ),
          ...env,
        };

        const logFile = Bun.file(logPath);
        const proc = Bun.spawn([resolved], {
          env: processEnv,
          stdout: logFile,
          stderr: logFile,
        });

        yield* logger.info(`Spawned ${name}`, { pid: proc.pid });

        // Write PID file
        if (pidFileService !== "") {
          yield* pidFile.write(pidFileService, proc.pid);
          yield* logger.info(`Wrote PID file for ${name}`, {
            pid: proc.pid,
            pidFile: pidFileService,
          });
        }

        return proc;
      });

    const signalAndWaitMethod = (
      subprocess: Bun.Subprocess,
      signal: NodeJS.Signals,
      timeoutMs: number,
    ): Effect.Effect<boolean, never> =>
      signalAndWaitUtil(subprocess.pid, signal, timeoutMs);

    const stopServiceByPIDFile = (
      name: string,
      pidFileService: string,
      patterns: ReadonlyArray<string>,
    ): Effect.Effect<void, ProcessError> =>
      Effect.gen(function* () {
        // Read PID from file
        const pid = yield* pidFile.read(pidFileService);
        if (pid === null) {
          return yield* Effect.fail(
            new ProcessError(`${name}: not running (PID file not found)`),
          );
        }

        // Check if process is alive
        const alive = yield* pidFile.isRunning(pid);
        if (!alive) {
          yield* pidFile.remove(pidFileService);
          return yield* Effect.fail(
            new ProcessError(
              `${name}: process not found (removing stale PID file)`,
            ),
          );
        }

        // Validate process pattern if provided
        if (patterns.length > 0) {
          const matches = yield* pidFile.processMatchesPattern(pid, patterns);
          if (!matches) {
            yield* pidFile.remove(pidFileService);
            return yield* Effect.fail(
              new ProcessError(
                `${name}: stale PID file (PID ${pid} does not match pattern, removed)`,
              ),
            );
          }
        }

        yield* logger.info(`Stopping ${name}`, { pid, signal: "SIGTERM" });
        const stopped = yield* signalAndWaitUtil(pid, "SIGTERM", 5000);
        if (!stopped) {
          yield* logger.warn(`${name}: did not exit gracefully, force-killed`, {
            pid,
          });
        }

        yield* logger.info(`${name}: Stopped`, { pid });

        // Remove PID file
        yield* pidFile.remove(pidFileService);
      });

    const cleanupStalePIDs = (
      services: ReadonlyArray<string>,
    ): Effect.Effect<void, never> => pidFile.cleanupStale(services);

    return {
      resolveServiceBinary,
      startService,
      stopServiceByPIDFile,
      signalAndWait: signalAndWaitMethod,
      cleanupStalePIDs,
    };
  }),
);
