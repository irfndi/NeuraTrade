import { stat } from "node:fs/promises";

function value(argv: readonly string[], name: string): string | null {
  const index = argv.indexOf(name);
  return index >= 0 ? (argv[index + 1] ?? null) : null;
}

async function main(): Promise<void> {
  const argv = process.argv.slice(2);
  const home = value(argv, "--home");
  const expectedExit = Number(value(argv, "--expect-exit") ?? "-1");
  const expectedStatus = value(argv, "--expect-status");
  const childArgs = JSON.parse(value(argv, "--argv-json") ?? "[]") as string[];
  if (home === null || expectedExit < 0 || expectedStatus === null) {
    throw new Error(
      "--home, --argv-json, --expect-exit, and --expect-status are required",
    );
  }
  const before = await stat(`${home}/data/neuratrade.db`).catch(() => null);
  const child = Bun.spawn(["bun", "run", "index.ts", ...childArgs], {
    cwd: import.meta.dir + "/../..",
    env: { ...process.env, NEURATRADE_HOME: home },
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr] = await Promise.all([
    new Response(child.stdout).text(),
    new Response(child.stderr).text(),
  ]);
  await child.exited;
  const report = JSON.parse(stdout) as { readonly status: string };
  if (child.exitCode !== expectedExit) {
    throw new Error(
      `expected exit ${expectedExit}, got ${child.exitCode}: ${stderr}`,
    );
  }
  if (report.status !== expectedStatus) {
    throw new Error(`expected status ${expectedStatus}, got ${report.status}`);
  }
  if (argv.includes("--assert-home-unchanged") && before !== null) {
    const after = await stat(`${home}/data/neuratrade.db`);
    if (after.size !== before.size || after.mtimeMs !== before.mtimeMs) {
      throw new Error("readiness command mutated the database file");
    }
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
