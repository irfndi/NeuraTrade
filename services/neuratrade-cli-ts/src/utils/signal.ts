import { Effect } from "effect";

/**
 * Send a signal to a process identified by PID and wait for it to exit
 * or for the timeout to elapse.
 *
 * Returns `true` if the process exited within the timeout, `false` if
 * the timeout elapsed before exit.
 *
 * If the PID does not exist (process already exited), returns `true`
 * immediately. If the signal cannot be sent (e.g. ESRCH), the function
 * treats the process as already gone and returns `true`.
 */
export function signalAndWait(
  pid: number,
  signal: NodeJS.Signals,
  timeoutMs: number
): Effect.Effect<boolean, never, never> {
  return Effect.gen(function* () {
    return yield* Effect.tryPromise({
      try: (): Promise<boolean> =>
        new Promise<boolean>((resolve) => {
          try {
            process.kill(pid, signal);
          } catch {
            resolve(true);
            return;
          }

          const timer = setTimeout(() => {
            try {
              process.kill(pid, "SIGKILL");
            } catch {
              // already dead
            }
            resolve(false);
          }, timeoutMs);

          const check = setInterval(() => {
            try {
              process.kill(pid, 0);
            } catch {
              clearTimeout(timer);
              clearInterval(check);
              resolve(true);
            }
          }, 10);
        }),
      catch: (): never => {
        throw new Error("unreachable");
      },
    });
  });
}
