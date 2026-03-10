# Beads - AI-Native Issue Tracking

Welcome to Beads! This repository uses **Beads** for issue tracking, with issue data living directly in the repo and synced through Beads' Dolt workflow alongside your code.

## What is Beads?

Beads is issue tracking that lives in your repo, making it a good fit for AI coding agents and developers who want their issues close to their code. No web UI required: everything works through the CLI, and syncing happens explicitly with `bd dolt commit` and `bd dolt push`.

**Learn more:** [github.com/steveyegge/beads](https://github.com/steveyegge/beads)

## Initial Setup

Beads uses a Dolt SQL server under the hood and expects a `dolt` binary to be installed separately and available on your `PATH`; it is not bundled by this repository. A quick sanity check is:

```bash
dolt version
bd dolt show
```

The basic Beads sync flow is:

```bash
bd dolt commit
bd dolt push
```

If this repo does not have a remote yet, configure one before the first `bd dolt push`:

```bash
bd dolt remote add <name> <url>
```

Example filesystem remote:

```bash
bd dolt remote add origin file:///Users/you/.beads-remotes/neuratrade
bd dolt push
```

For Dolt installation and remote configuration details, see the official docs:
- [Installation](https://docs.dolthub.com/introduction/installation)
- [Using Remotes](https://docs.dolthub.com/sql-reference/version-control/remotes)

## Quick Start

### Essential Commands

```bash
# Create new issues
bd create "Add user authentication"

# View all issues
bd list

# View issue details
bd show <issue-id>

# Update issue status
bd update <issue-id> --status in_progress
bd update <issue-id> --status done

# Commit Beads changes locally
bd dolt commit

# Push to configured Dolt remote
bd dolt push
```

### QA Gate Closure

For this repository, close issues with QA evidence using:

```bash
ISSUE_ID=<bd-id> \
UNIT_TESTS="..." \
INTEGRATION_TESTS="..." \
E2E_TESTS="..." \
COVERAGE_RESULT="..." \
EVIDENCE="..." \
make bd-close-qa
```

This records QA evidence in issue notes and then closes the issue.

### Working with Issues

Issues in Beads are:
- **Repo-local**: Stored in `.beads/issues.jsonl`
- **AI-friendly**: CLI-first design works perfectly with AI coding agents
- **Branch-aware**: Issues can follow your branch workflow
- **Explicitly synced**: Run `bd dolt commit` locally, then `bd dolt push` to sync with the configured Dolt remote

## Why Beads?

✨ **AI-Native Design**
- Built specifically for AI-assisted development workflows
- CLI-first interface works seamlessly with AI coding agents
- No context switching to web UIs

🚀 **Developer Focused**
- Issues live in your repo, right next to your code
- Works offline — commit local changes with `bd dolt commit`, then sync them with `bd dolt push`
- Fast, lightweight, and stays out of your way

🔧 **Dolt Workflow**
- Explicit local issue commits with `bd dolt commit`
- Explicit remote sync with `bd dolt push`
- Branch-aware issue tracking with JSONL merge resolution

## Get Started with Beads

Try Beads in your own projects:

```bash
# Install Beads
curl -sSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash

# Initialize in your repo
bd init

# Create your first issue
bd create "Try out Beads"
```

## Learn More

- **Documentation**: [github.com/steveyegge/beads/docs](https://github.com/steveyegge/beads/tree/main/docs)
- **Quick Start Guide**: Run `bd quickstart`
- **Examples**: [github.com/steveyegge/beads/examples](https://github.com/steveyegge/beads/tree/main/examples)

---

*Beads: Issue tracking that moves at the speed of thought* ⚡
