WITH duplicate_rows AS (
    SELECT id
    FROM (
        SELECT id,
               ROW_NUMBER() OVER (
                   PARTITION BY shadow_decision_id
                   ORDER BY COALESCE(closed_at, created_at) DESC, id DESC
               ) AS rn
        FROM shadow_outcomes
    ) ranked
    WHERE rn > 1
)
DELETE FROM shadow_outcomes
WHERE id IN (SELECT id FROM duplicate_rows);

DROP INDEX IF EXISTS idx_shadow_outcomes_shadow_decision_id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_shadow_outcomes_shadow_decision_id_unique
    ON shadow_outcomes (shadow_decision_id);
