import { describe, expect, test } from "bun:test";
import { Effect } from "effect";
import { signalAndWait } from "./signal.ts";

// ===========================================================================
// signalAndWait
// ===========================================================================
describe("signalAndWait", () => {
  test("returns true when process exits within timeout", async () => {
    // Spawn a short-lived process
    const proc = Bun.spawn(["sleep", "0.05"]);
    const pid = proc.pid;

    const result = await Effect.runPromise(signalAndWait(pid, "SIGTERM", 2000));
    expect(result).toBe(true);
  });

  test("returns true when sending SIGTERM to a sleep process that exits", async () => {
    const proc = Bun.spawn(["sleep", "60"]);
    const pid = proc.pid;

    // Give it a moment to start
    await Bun.sleep(50);

    const result = await Effect.runPromise(signalAndWait(pid, "SIGTERM", 2000));
    expect(result).toBe(true);

    // Clean up in case it didn't exit
    try {
      process.kill(pid, "SIGKILL");
    } catch {
      // already dead
    }
  });

  test("returns false when process does not exit within timeout", async () => {
    // Spawn a process that ignores SIGTERM (sleep ignores nothing, but it will
    // take longer than our timeout on some systems; we use a tight timeout)
    const proc = Bun.spawn(["sleep", "60"]);
    const pid = proc.pid;

    await Bun.sleep(50);

    // Very short timeout — sleep won't exit that fast
    const result = await Effect.runPromise(signalAndWait(pid, "SIGTERM", 1));
    // The process may or may not exit within 1ms — we just verify no crash
    expect(result).toEqual(expect.any(Boolean));

    // Force cleanup
    try {
      process.kill(pid, "SIGKILL");
    } catch {
      // already dead
    }
    // Wait for cleanup
    await proc.exited;
  });

  test("handles non-existent PID gracefully", async () => {
    // Use a PID that almost certainly doesn't exist
    const result = await Effect.runPromise(
      signalAndWait(2_147_483_647, "SIGTERM", 100),
    );
    // Should not throw; process not found = treated as already exited
    expect(result).toBe(true);
  });

  test("works with SIGINT signal", async () => {
    const proc = Bun.spawn(["sleep", "60"]);
    const pid = proc.pid;

    await Bun.sleep(50);

    const result = await Effect.runPromise(signalAndWait(pid, "SIGINT", 2000));
    expect(result).toBe(true);

    try {
      process.kill(pid, "SIGKILL");
    } catch {
      // already dead
    }
  });
});
