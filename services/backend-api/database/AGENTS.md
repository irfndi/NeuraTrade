# DATABASE KNOWLEDGE BASE

## OVERVIEW
`database/` contains migration tooling and ordered SQL migrations for backend schema evolution.
`migrate.sh` is the canonical migration entrypoint for local and CI workflows.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Run/list/status migrations | `migrate.sh` | Supports `run`, `status`, `list`, numeric target |
| Migration files | `migrations/*.sql` | Sequential naming and deterministic ordering |
| Migration conventions | `README.md` | Naming and operational notes |

## MIGRATION RULES
- File naming: `NNN_descriptive_name.sql` with zero-padded sequence.
- Migrations are executed in version order (`sort -V` behavior in scripts).
- Write idempotent SQL where practical (`IF NOT EXISTS`, guarded blocks).
- Avoid editing historical migration semantics unless absolutely required for repair migration.
- Prefer additive forward migrations over in-place rewrites.

## COMMANDS
```bash
./migrate.sh run
./migrate.sh status
./migrate.sh list
./migrate.sh 052
```

## GOTCHAS
- `rollback` in `migrate.sh` is limited; plan explicit rollback SQL when needed.
- Conflicting migration history has been handled before (see consolidation migrations); preserve ordering discipline.
- Validate both fresh-install and upgrade paths when adding schema changes.

## BACKLOG (bd CLI)

**Stats:** 185 total | 58 open | 8 in progress | 33 blocked | 119 closed | 28 ready

### Recently Completed (✓)
- ✓ `neura-5of`: Telegram profile binding schema (operator_identities table)
- ✓ `neura-ydp`: Operator identity encryption (Argon2 + encrypted fields)
- ✓ `neura-yus`: API keys encryption at rest (AES-256-GCM)
- ✓ `neura-nh5`: Risk event notification log schema
- ✓ `neura-px6`: Security audit data retention tables
- ✓ `neura-9ai`: Intrusion detection audit log schema
- ✓ `neura-2n4`: Quest state persistence schema (quests, checkpoints, progress tables)
- ✓ `neura-32w`: Fund with minimal capital schema (USDC)
- ✓ `neura-06k`: Health checks schema

### Schema Evolution (Blocked)
- `neura-zn8c`: Replace in-memory trading handler state with persistent storage (new tables)
- `neura-8de`: Virtual account tracking schema (paper trading accounts)
- `neura-mm5`: Paper trade recording schema (paper_trades table)
- `neura-u4w`: Paper execution simulation schema

## ANTI-PATTERNS
- Renumbering existing migrations after they are shared.
- Embedding environment-specific assumptions in SQL.
- Creating non-idempotent migrations that fail on re-run in recovery scenarios.
