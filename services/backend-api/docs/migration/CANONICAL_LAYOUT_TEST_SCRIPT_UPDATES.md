# Canonical Layout Test and Script Updates

## Applied Updates
- Canonical handler tests now include `services/backend-api/internal/api/handlers/trading_test.go`.
- Coverage target list remains canonical in `services/backend-api/scripts/coverage-check.sh` and does not include legacy `./internal/handlers`.
- Legacy path guardrail script is active at `services/backend-api/scripts/check-legacy-paths.sh`.
- Root CI command path includes structure checks through `Makefile` target `ci-structure-check`.
- Module metadata refreshed with `go mod tidy`.

## Verification Commands
- `go test ./services/backend-api/internal/api/...`
- `go test ./services/backend-api/internal/api/handlers/...`
- `go test ./services/backend-api/...`
- `bash services/backend-api/scripts/check-legacy-paths.sh`
- `STRICT=false bash services/backend-api/scripts/coverage-check.sh`

## Notes
- Coverage threshold remains warning-mode by default unless `STRICT=true`.
- Coverage baseline/delta enforcement has follow-up tracking in `neura-1xx`.
