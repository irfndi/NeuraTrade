CREATE TABLE IF NOT EXISTS live_readiness_manifests (
    id TEXT PRIMARY KEY,
    manifest_json TEXT NOT NULL,
    acceptance_ready BOOLEAN NOT NULL DEFAULT FALSE,
    acceptance_failures TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_live_readiness_manifests_acceptance_ready
    ON live_readiness_manifests(acceptance_ready, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_live_readiness_manifests_created_at
    ON live_readiness_manifests(created_at DESC);

CREATE TABLE IF NOT EXISTS live_readiness_manifest_strategies (
    manifest_id TEXT NOT NULL,
    strategy TEXT NOT NULL,
    strategy_json TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (manifest_id, strategy),
    FOREIGN KEY (manifest_id) REFERENCES live_readiness_manifests(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_live_readiness_manifest_strategies_strategy
    ON live_readiness_manifest_strategies(strategy, created_at DESC);
