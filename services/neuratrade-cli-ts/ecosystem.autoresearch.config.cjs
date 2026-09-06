/**
 * PM2: 4 parallel autoresearch workers sharing champion.json via lockfile.
 * Panel cached in-process; screen→confirm two-phase.
 */
module.exports = {
  apps: [0, 1, 2, 3].map((worker) => ({
    name: `neuratrade-autoresearch-w${worker}`,
    cwd: __dirname,
    script: "bun",
    interpreter: "none",
    args: [
      "run",
      "autoresearch/loop.ts",
      `--worker=${worker}`,
      "--workers=4",
      "--trials=50000",
      "--symbols=8",
      "--screen-steps=12",
      "--screen-budget-sec=45",
      "--confirm-steps=40",
      "--confirm-budget-sec=180",
    ],
    autorestart: true,
    max_restarts: 100,
    max_memory_restart: "2G",
    out_file: `${process.env.HOME || "/root"}/.neuratrade/logs/autoresearch-w${worker}.out.log`,
    error_file: `${process.env.HOME || "/root"}/.neuratrade/logs/autoresearch-w${worker}.err.log`,
    time: true,
  })),
};
