# NeuraTrade Documentation

Welcome to the NeuraTrade documentation. This directory contains comprehensive guides for operators, developers, and security teams.

## Quick Start

| Document | Description | Audience |
|----------|-------------|----------|
| [Operator Guide](./OPERATOR_GUIDE.md) | System operations, service management, monitoring | Operators, DevOps |
| [Troubleshooting](./TROUBLESHOOTING.md) | Common issues, diagnosis, solutions | All users |
| [Security](./SECURITY.md) | Security architecture, best practices, incident response | Security teams, Developers |

## Documentation Index

### Operations

| Document | Description |
|----------|-------------|
| [Operator Guide](./OPERATOR_GUIDE.md) | Complete operational reference covering service management, health monitoring, database/Redis operations, exchange connectivity, trading operations, and runbooks. |
| [Troubleshooting](./TROUBLESHOOTING.md) | Comprehensive troubleshooting guide with diagnosis steps, common issues, and solutions for services, database, Redis, exchanges, Telegram, and more. |

### Security

| Document | Description |
|----------|-------------|
| [Security Documentation](./SECURITY.md) | Security architecture, authentication/authorization, data protection, secrets management, network security, trading security, monitoring, and incident response. |

### Planning & Architecture

| Document | Description |
|----------|-------------|
| [Master Plan](./Master_Plan.md) | Project roadmap and implementation phases |
| [Testing Strategy](./TESTING_STRATEGY.md) | Testing approach and coverage requirements |
| [BD Implementation Plan](./BD_BEADS_IMPLEMENTATION_PLAN.md) | Backlog management implementation |
| [DB Abstraction Plan](./IMPLEMENTATION_PLAN_DB_ABSTRACTION.md) | Database abstraction layer design |

### Legacy Documentation

Historical documentation is preserved in the `old/` directory:

```
old/
├── architecture/      # Architecture recommendations and analysis
├── deployment/        # Deployment guides (superseded)
├── development/       # Development guides (superseded)
├── security/          # Security docs (consolidated into SECURITY.md)
└── troubleshooting/   # Troubleshooting (consolidated into TROUBLESHOOTING.md)
```

## Service-Specific Documentation

Each service has its own `AGENTS.md` file with detailed implementation guidance:

| Service | Location |
|---------|----------|
| Backend API | [services/backend-api/AGENTS.md](../services/backend-api/AGENTS.md) |
| CCXT Service | [services/ccxt-service/AGENTS.md](../services/ccxt-service/AGENTS.md) |
| Telegram Service | [services/telegram-service/AGENTS.md](../services/telegram-service/AGENTS.md) |

## Getting Help

### For Operators

1. Start with the [Operator Guide](./OPERATOR_GUIDE.md)
2. If issues arise, check [Troubleshooting](./TROUBLESHOOTING.md)
3. For security concerns, see [Security](./SECURITY.md)

### For Developers

1. Review the main [README](../README.md) for project overview
2. Check service-specific AGENTS.md files
3. See [.github/copilot-instructions.md](../.github/copilot-instructions.md) for coding guidelines

### For Security Teams

1. Primary reference: [Security Documentation](./SECURITY.md)
2. Report issues: security@neuratrade.com
3. See incident response procedures in the security docs

## Documentation Maintenance

- **Update frequency**: Documentation should be updated with each significant change
- **Review schedule**: Quarterly review for accuracy
- **Ownership**: Each doc has a designated maintainer

## Contributing

To contribute to documentation:

1. Follow the existing structure and style
2. Keep documents concise and actionable
3. Include code examples where appropriate
4. Test all commands and procedures
5. Submit via pull request with description

## Changelog

| Date | Change |
|------|--------|
| 2026-02-13 | Created consolidated docs: OPERATOR_GUIDE.md, TROUBLESHOOTING.md, SECURITY.md |
| 2026-02-13 | Moved legacy docs to old/ directory |
| 2026-02-11 | Initial documentation structure |
