import * as os from "os";
import * as nodePath from "path";
import { Context, Layer } from "effect";

/**
 * Resolves the base home directory from the given input.
 *
 * Priority:
 * 1. Explicit `homeDir` argument (if provided and non-empty)
 * 2. `NEURATRADE_HOME` environment variable
 * 3. Default: `~/.neuratrade`
 *
 * Leading `~` is expanded to the OS home directory.
 */
function resolveHome(explicitHome?: string): string {
  let raw: string;
  if (explicitHome && explicitHome.length > 0) {
    raw = explicitHome;
  } else if (
    process.env.NEURATRADE_HOME &&
    process.env.NEURATRADE_HOME.length > 0
  ) {
    raw = process.env.NEURATRADE_HOME;
  } else {
    raw = "~/.neuratrade";
  }

  if (raw.startsWith("~")) {
    return nodePath.join(os.homedir(), raw.slice(1));
  }
  return raw;
}

/**
 * The Path service interface. Resolves all NeuraTrade file paths
 * from the home directory (NEURATRADE_HOME).
 */
export class Path extends Context.Service<
  Path,
  {
    /** The resolved NeuraTrade home directory. */
    readonly homeDir: string;
    /** `$home/pids` — PID file directory. */
    readonly pidDir: string;
    /** `$home/logs` — Log file directory. */
    readonly logDir: string;
    /** `$home/data` — Data file directory (SQLite DB, etc.). */
    readonly dataDir: string;
    /** `$home/config.json` — Secret config file. */
    readonly configPath: string;
    /** `$home/runtime.json` — Runtime config file. */
    readonly runtimeConfigPath: string;
    /** `$home/pids/gateway-state.json` — Gateway state file. */
    readonly gatewayStatePath: string;
    /** Returns the PID file path for the given service name. */
    readonly pidFilePath: (service: string) => string;
  }
>()("Path") {}

/**
 * Create a Path service Layer.
 *
 * @param homeDir - Optional explicit home directory. Takes precedence over
 *   `NEURATRADE_HOME` env var. Supports `~` expansion. When omitted, falls
 *   back to `NEURATRADE_HOME` then `~/.neuratrade`.
 */
export const PathLive = (homeDir?: string): Layer.Layer<Path> => {
  const resolvedHome = resolveHome(homeDir);
  return Layer.succeed(Path, {
    homeDir: resolvedHome,
    pidDir: nodePath.join(resolvedHome, "pids"),
    logDir: nodePath.join(resolvedHome, "logs"),
    dataDir: nodePath.join(resolvedHome, "data"),
    configPath: nodePath.join(resolvedHome, "config.json"),
    runtimeConfigPath: nodePath.join(resolvedHome, "runtime.json"),
    gatewayStatePath: nodePath.join(resolvedHome, "pids", "gateway-state.json"),
    pidFilePath: (service: string) =>
      nodePath.join(resolvedHome, "pids", `${service}.pid`),
  });
};
