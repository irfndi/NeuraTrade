import { delimiter, resolve } from "node:path";
import { z } from "zod";

export interface TimesFmSeriesInput {
  readonly id: string;
  readonly timestampsMs: readonly number[];
  readonly targets: readonly (readonly number[])[];
  readonly targetNames?: readonly string[];
  readonly pastOnlyCovariates?: readonly (readonly number[])[];
  readonly pastFutureCovariates?: readonly (readonly number[])[];
}

export interface TimesFmForecastRequest {
  readonly requestId: string;
  readonly horizon: number;
  readonly intervalMs: number;
  readonly series: readonly TimesFmSeriesInput[];
  readonly returnQuantiles?: boolean;
  readonly useSymmetricAveraging?: boolean;
  readonly useZnorm?: boolean;
}

export interface TimesFmForecastRecord {
  readonly id: string;
  readonly targetNames: readonly string[];
  readonly timestampsMs: readonly number[];
  readonly forecast: readonly (readonly number[])[];
  readonly quantiles: readonly (readonly (readonly number[])[])[] | null;
}

export interface TimesFmForecastResponse {
  readonly ok: true;
  readonly requestId: string;
  readonly latencyMs: number;
  readonly forecasts: readonly TimesFmForecastRecord[];
}

export interface TimesFmValidatedResponse {
  readonly ok: true;
  readonly requestId: string;
  readonly validated: true;
  readonly seriesCount: number;
  readonly horizon: number;
}

export type TimesFmWorkerResponse =
  | TimesFmForecastResponse
  | TimesFmValidatedResponse;

export interface TimesFmWorkerOptions {
  readonly projectDir?: string;
  readonly checkpoint?: string;
  readonly device?: string;
  readonly batchSize?: number;
  readonly cacheDir?: string;
  readonly localFilesOnly?: boolean;
  readonly torchThreads?: number;
  readonly validateOnly?: boolean;
  readonly requestTimeoutMs?: number;
}

export class TimesFmWorkerError extends Error {
  readonly code: string;

  constructor(message: string, code = "worker_error") {
    super(message);
    this.name = "TimesFmWorkerError";
    this.code = code;
  }
}

type SpawnedProcess = ReturnType<typeof Bun.spawn>;

const finiteNumberSchema = z.number().finite();
const numberRowSchema = z.array(finiteNumberSchema);
const quantilesSchema = z.array(z.array(numberRowSchema));
const forecastRecordSchema = z.object({
  id: z.string().min(1),
  target_names: z.array(z.string()),
  timestamps_ms: z.array(finiteNumberSchema),
  forecast: z.array(numberRowSchema),
  quantiles: quantilesSchema.nullable(),
});
const forecastResponseSchema = z.object({
  ok: z.literal(true),
  request_id: z.string().min(1),
  latency_ms: finiteNumberSchema,
  forecasts: z.array(forecastRecordSchema),
});
const validatedResponseSchema = z.object({
  ok: z.literal(true),
  request_id: z.string().min(1),
  validated: z.literal(true),
  series_count: finiteNumberSchema,
  horizon: finiteNumberSchema,
});
const errorResponseSchema = z.object({
  ok: z.literal(false),
  request_id: z.string().nullable(),
  error: z.object({ code: z.string(), message: z.string() }),
});
const workerResponseSchema = z.union([
  forecastResponseSchema,
  validatedResponseSchema,
  errorResponseSchema,
]);
type WorkerWireResponse = z.infer<typeof workerResponseSchema>;

function parseForecastResponse(
  value: WorkerWireResponse,
): TimesFmWorkerResponse {
  if (!value.ok) {
    throw new TimesFmWorkerError(value.error.message, value.error.code);
  }
  if ("validated" in value) {
    return {
      ok: true,
      requestId: value.request_id,
      validated: true,
      seriesCount: value.series_count,
      horizon: value.horizon,
    };
  }
  return {
    ok: true,
    requestId: value.request_id,
    latencyMs: value.latency_ms,
    forecasts: value.forecasts.map((record) => ({
      id: record.id,
      targetNames: record.target_names,
      timestampsMs: record.timestamps_ms,
      forecast: record.forecast,
      quantiles: record.quantiles,
    })),
  };
}

function wireRequest(request: TimesFmForecastRequest) {
  return {
    request_id: request.requestId,
    horizon: request.horizon,
    interval_ms: request.intervalMs,
    series: request.series.map((series) => ({
      id: series.id,
      timestamps_ms: series.timestampsMs,
      targets: series.targets,
      target_names: series.targetNames,
      past_only_covariates: series.pastOnlyCovariates,
      past_future_covariates: series.pastFutureCovariates,
    })),
    return_quantiles: request.returnQuantiles ?? true,
    use_symmetric_averaging: request.useSymmetricAveraging ?? true,
    use_znorm: request.useZnorm ?? false,
  };
}

export class TimesFmWorker {
  private process: SpawnedProcess | undefined;
  private stdoutReader: ReadableStreamDefaultReader<Uint8Array> | undefined;
  private stdoutBuffer = "";
  private stderrTail = "";
  private stderrTask: Promise<void> | undefined;

  constructor(private readonly options: TimesFmWorkerOptions = {}) {}

  private projectDir(): string {
    return resolve(
      this.options.projectDir ??
        process.env.NEURATRADE_TIMESFM_PROJECT ??
        resolve(import.meta.dir, "../../../timesfm-forecast"),
    );
  }

  private command(): string[] {
    const command = [
      process.env.NEURATRADE_UV_BIN ?? "uv",
      "run",
      "--project",
      this.projectDir(),
      "--locked",
    ];
    if (this.options.validateOnly === true) command.push("--no-sync");
    command.push(
      "python",
      "-m",
      "neuratrade_timesfm.worker",
      "--checkpoint",
      this.options.checkpoint ?? "google/timesfm-3.0-pytorch",
      "--device",
      this.options.device ?? "auto",
      "--batch-size",
      String(this.options.batchSize ?? 4),
    );
    if (this.options.cacheDir !== undefined) {
      command.push("--cache-dir", this.options.cacheDir);
    }
    if (this.options.localFilesOnly === true) {
      command.push("--local-files-only");
    }
    if ((this.options.torchThreads ?? 0) > 0) {
      command.push("--torch-threads", String(this.options.torchThreads));
    }
    if (this.options.validateOnly === true) command.push("--validate-only");
    return command;
  }

  private async drainStderr(stream: ReadableStream<Uint8Array>): Promise<void> {
    const reader = stream.getReader();
    const decoder = new TextDecoder();
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        this.stderrTail =
          `${this.stderrTail}${decoder.decode(value, { stream: true })}`
            .slice(-2000)
            .trim();
      }
    } finally {
      reader.releaseLock();
    }
  }

  private async start(): Promise<void> {
    if (this.process !== undefined) return;
    let processHandle: SpawnedProcess;
    try {
      processHandle = Bun.spawn(this.command(), {
        cwd: this.projectDir(),
        env: {
          ...process.env,
          PYTHONPATH: [
            resolve(this.projectDir(), "src"),
            process.env.PYTHONPATH,
          ]
            .filter((value): value is string => value !== undefined)
            .join(delimiter),
          PYTHONUNBUFFERED: "1",
        },
        stdin: "pipe",
        stdout: "pipe",
        stderr: "pipe",
      });
    } catch (error) {
      throw new TimesFmWorkerError(
        `failed to start uv TimesFM worker: ${error instanceof Error ? error.message : String(error)}`,
        "spawn_error",
      );
    }
    if (!(processHandle.stdout instanceof ReadableStream)) {
      processHandle.kill();
      throw new TimesFmWorkerError("TimesFM worker stdout is not a pipe");
    }
    if (!(processHandle.stderr instanceof ReadableStream)) {
      processHandle.kill();
      throw new TimesFmWorkerError("TimesFM worker stderr is not a pipe");
    }
    this.process = processHandle;
    this.stdoutReader = processHandle.stdout.getReader();
    this.stderrTask = this.drainStderr(processHandle.stderr);
    void this.stderrTask.catch(() => undefined);
  }

  private async readLine(): Promise<string> {
    const reader = this.stdoutReader;
    if (reader === undefined) {
      throw new TimesFmWorkerError("TimesFM worker stdout is unavailable");
    }
    const decoder = new TextDecoder();
    while (true) {
      const lineEnd = this.stdoutBuffer.indexOf("\n");
      if (lineEnd >= 0) {
        const line = this.stdoutBuffer.slice(0, lineEnd).replace(/\r$/, "");
        this.stdoutBuffer = this.stdoutBuffer.slice(lineEnd + 1);
        return line;
      }
      const { done, value } = await reader.read();
      if (done) {
        this.stdoutBuffer += decoder.decode();
        const tail = this.stdoutBuffer.trim();
        const detail = this.stderrTail.length > 0 ? `: ${this.stderrTail}` : "";
        throw new TimesFmWorkerError(
          `TimesFM worker exited before responding${tail.length > 0 ? ` (${tail})` : ""}${detail}`,
          "worker_exit",
        );
      }
      this.stdoutBuffer += decoder.decode(value, { stream: true });
    }
  }

  async request(
    request: TimesFmForecastRequest,
  ): Promise<TimesFmWorkerResponse> {
    await this.start();
    const processHandle = this.process;
    if (processHandle === undefined || processHandle.stdin === undefined) {
      throw new TimesFmWorkerError("TimesFM worker stdin is unavailable");
    }
    // `stdin: "pipe"` above guarantees a FileSink at runtime; Bun's broad
    // Subprocess type also includes inherited file descriptors.
    const stdin = processHandle.stdin as Exclude<
      typeof processHandle.stdin,
      number
    >;
    try {
      await stdin.write(`${JSON.stringify(wireRequest(request))}\n`);
      const timeoutMs = this.options.requestTimeoutMs ?? 120_000;
      let timeoutHandle: ReturnType<typeof setTimeout> | undefined;
      try {
        const response = await Promise.race([
          this.readLine(),
          new Promise<never>((_, reject) => {
            timeoutHandle = setTimeout(
              () =>
                reject(
                  new TimesFmWorkerError(
                    `TimesFM worker timed out after ${timeoutMs}ms`,
                    "timeout",
                  ),
                ),
              timeoutMs,
            );
          }),
        ]);
        const parsed = workerResponseSchema.safeParse(
          JSON.parse(response) as unknown,
        );
        if (!parsed.success) {
          throw new TimesFmWorkerError(
            "worker response failed schema validation",
            "schema_error",
          );
        }
        return parseForecastResponse(parsed.data);
      } finally {
        if (timeoutHandle !== undefined) clearTimeout(timeoutHandle);
      }
    } catch (error) {
      await this.close();
      if (error instanceof TimesFmWorkerError) throw error;
      throw new TimesFmWorkerError(
        `TimesFM worker request failed: ${error instanceof Error ? error.message : String(error)}`,
        "request_error",
      );
    }
  }

  async close(): Promise<void> {
    const processHandle = this.process;
    this.process = undefined;
    this.stdoutReader?.releaseLock();
    this.stdoutReader = undefined;
    if (processHandle !== undefined) {
      processHandle.kill();
      await processHandle.exited.catch(() => undefined);
    }
    await this.stderrTask?.catch(() => undefined);
    this.stderrTask = undefined;
  }
}
