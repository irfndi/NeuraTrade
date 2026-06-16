import { Context, Effect, Layer } from "effect";

export interface ProbeHTTPResult {
  readonly healthy: boolean;
  readonly detail: string;
}

export interface ProbeProcessResult {
  readonly running: boolean;
  readonly detail: string;
}

export interface HealthJSONResult {
  readonly ok: boolean;
  readonly status: string;
  readonly services?: Record<string, string>;
  readonly error?: string;
}

export interface HealthCheck {
  /** GET with timeout; healthy when status is 2xx–4xx (mirrors Go >= 200 && < 500). */
  readonly probeHTTP: (
    url: string,
    timeoutMs: number,
  ) => Effect.Effect<ProbeHTTPResult, never, never>;

  /** Poll until healthy or timeout. First probe fires immediately; spaced by intervalMs (default 500ms). */
  readonly waitForHealthy: (
    url: string,
    timeoutMs: number,
    intervalMs?: number,
  ) => Effect.Effect<ProbeHTTPResult, never, never>;

  /** pgrep -f pattern; running when exit 0 with non-empty output (mirrors Go checkProcess). */
  readonly probeProcess: (
    pattern: string,
  ) => Effect.Effect<ProbeProcessResult, never, never>;

  /** GET /health style JSON and parse { status, services }. */
  readonly probeHealthJSON: (
    url: string,
    timeoutMs: number,
  ) => Effect.Effect<HealthJSONResult, never, never>;
}

export const HealthCheck = Context.GenericTag<HealthCheck>("HealthCheck");

const DEFAULT_INTERVAL_MS = 500;

function probeHTTPOnce(
  url: string,
  timeoutMs: number,
): Effect.Effect<ProbeHTTPResult, never, never> {
  return Effect.gen(function* () {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    try {
      const outcome = yield* Effect.tryPromise({
        try: () =>
          fetch(url, {
            method: "GET",
            signal: controller.signal,
            redirect: "follow",
          }),
        catch: (err) =>
          new Error(
            `HTTP probe failed for ${url}: ${
              err instanceof Error ? err.message : String(err)
            }`,
          ),
      }).pipe(
        Effect.catchAll((err) =>
          Effect.succeed({ _failed: true, error: err } as {
            _failed: true;
            error: Error;
          }),
        ),
      );

      if ("_failed" in outcome) {
        return {
          healthy: false,
          detail: outcome.error.message,
        };
      }

      const status = outcome.status;
      return {
        healthy: status >= 200 && status < 500,
        detail: `HTTP ${status} from ${url}`,
      };
    } finally {
      clearTimeout(timer);
    }
  });
}

function probeProcessOnce(
  pattern: string,
): Effect.Effect<ProbeProcessResult, never, never> {
  return Effect.gen(function* () {
    const outcome = yield* Effect.tryPromise({
      try: async () => {
        const proc = Bun.spawn(["pgrep", "-f", pattern], {
          stdout: "pipe",
          stderr: "pipe",
        });
        const exitCode = await proc.exited;

        let stdout = "";
        try {
          const reader = proc.stdout.getReader();
          const decoder = new TextDecoder();
          let done = false;
          while (!done) {
            const { value, done: readerDone } = await reader.read();
            done = readerDone;
            if (value) stdout += decoder.decode(value, { stream: !done });
          }
        } catch {
          // pipe closed or decoder error — treat as empty output
        }

        return { exitCode, stdout: stdout.trim() };
      },
      catch: (err) =>
        new Error(
          `pgrep failed for pattern "${pattern}": ${
            err instanceof Error ? err.message : String(err)
          }`,
        ),
    }).pipe(
      Effect.catchAll((err) =>
        Effect.succeed({
          exitCode: 1,
          stdout: "",
          error: err as Error,
        }),
      ),
    );

    if ("error" in outcome && outcome.error) {
      return {
        running: false,
        detail: `pgrep failed: ${outcome.error.message}`,
      };
    }

    const pids = outcome.stdout
      .split("\n")
      .map((line: string) => line.trim())
      .filter((line: string) => line.length > 0);

    if (outcome.exitCode === 0 && pids.length > 0) {
      return {
        running: true,
        detail: `found ${pids.length} process(es) matching "${pattern}" (PIDs: ${pids.join(", ")})`,
      };
    }

    return {
      running: false,
      detail: `no processes found matching "${pattern}"`,
    };
  });
}

function probeHealthJSONOnce(
  url: string,
  timeoutMs: number,
): Effect.Effect<HealthJSONResult, never, never> {
  return Effect.gen(function* () {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    try {
      const outcome = yield* Effect.tryPromise({
        try: () =>
          fetch(url, {
            method: "GET",
            signal: controller.signal,
            redirect: "follow",
            headers: { Accept: "application/json" },
          }),
        catch: (err) =>
          new Error(
            `Health JSON probe failed for ${url}: ${
              err instanceof Error ? err.message : String(err)
            }`,
          ),
      }).pipe(
        Effect.catchAll((err) =>
          Effect.succeed({ _failed: true, error: err } as {
            _failed: true;
            error: Error;
          }),
        ),
      );

      if ("_failed" in outcome) {
        return {
          ok: false,
          status: "unreachable",
          error: outcome.error.message,
        };
      }

      if (!outcome.ok) {
        return {
          ok: false,
          status: String(outcome.status),
          error: `HTTP ${outcome.status} from ${url}`,
        };
      }

      const textResult = yield* Effect.tryPromise({
        try: () => outcome.text(),
        catch: () => new Error("Failed to read response body"),
      }).pipe(Effect.catchAll(() => Effect.succeed("")));

      let parsed: Record<string, unknown> = {};
      try {
        parsed = JSON.parse(textResult) as Record<string, unknown>;
      } catch {
        // ignore parse errors — fall back to empty object
      }

      const status =
        typeof parsed.status === "string" ? parsed.status : "unknown";
      const services: Record<string, string> = {};
      if (
        parsed.services &&
        typeof parsed.services === "object" &&
        !Array.isArray(parsed.services)
      ) {
        for (const [key, value] of Object.entries(parsed.services)) {
          services[key] = String(value);
        }
      }

      return { ok: true, status, services };
    } finally {
      clearTimeout(timer);
    }
  });
}

export const HealthCheckLive: Layer.Layer<HealthCheck> = Layer.succeed(
  HealthCheck,
  {
    probeHTTP(url, timeoutMs) {
      return probeHTTPOnce(url, timeoutMs);
    },

    waitForHealthy(url, timeoutMs, intervalMs) {
      const interval = intervalMs ?? DEFAULT_INTERVAL_MS;

      return Effect.gen(function* () {
        const deadline = Date.now() + timeoutMs;
        let lastResult: ProbeHTTPResult = {
          healthy: false,
          detail: "no response yet",
        };

        while (Date.now() < deadline) {
          const result = yield* probeHTTPOnce(url, 2000);
          if (result.healthy) {
            return result;
          }
          lastResult = result;

          const remaining = deadline - Date.now();
          const sleepMs = Math.min(interval, remaining);
          if (sleepMs > 0) {
            yield* Effect.sleep(`${sleepMs} millis`);
          }
        }

        return {
          healthy: false,
          detail: `health check failed at ${url} within ${timeoutMs}ms (${lastResult.detail})`,
        };
      });
    },

    probeProcess(pattern) {
      return probeProcessOnce(pattern);
    },

    probeHealthJSON(url, timeoutMs) {
      return probeHealthJSONOnce(url, timeoutMs);
    },
  },
);
