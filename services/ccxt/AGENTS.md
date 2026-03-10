# CCXT SERVICE KNOWLEDGE BASE

## OVERVIEW
`services/ccxt/` is a minimal CCXT exchange bridge. It provides HTTP/gRPC interfaces for exchange API calls, wrapping CCXT library functionality.

## STRUCTURE
```text
ccxt/
├── dist/           # Compiled output
├── node_modules/   # Dependencies
├── .env            # Configuration (gitignored)
└── README.md       # Service documentation
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Exchange API integration | Backend uses `internal/ccxt/` | CCXT client logic is in backend-api |
| Protobuf definitions | `protos/` (root) | Shared gRPC contracts |
| Build output | `dist/` | Compiled JavaScript |

## COMMANDS
```bash
bun install
bun run build
```

## CONVENTIONS
- This is a minimal service stub; primary CCXT integration lives in `services/backend-api/internal/ccxt/`
- Exchange credentials are managed by backend-api, not this service
- gRPC server handles exchange requests from backend

## BACKLOG (bd CLI)

**Stats:** 312 total | 64 open | 1 in progress | 14 blocked | 247 closed | 50 ready

## ANTI-PATTERNS
- Storing exchange API keys in this service.
- Bypassing backend-api rate limiting by direct CCXT calls.
