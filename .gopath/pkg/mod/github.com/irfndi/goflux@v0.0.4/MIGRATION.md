# Migration from Techan

## Overview

GoFlux is a maintained fork of [sdcoffey/techan](https://github.com/sdcoffey/techan).

## What Changed

- **Module path**: `github.com/sdcoffey/techan` → `github.com/irfndi/goflux`
- **Package name**: `techan` → `goflux`
- **Tooling**: standardized Make targets (and Docker-based workflows)

## Migration Guide

Update imports:

```go
// Before
import "github.com/sdcoffey/techan"

// After
import "github.com/irfndi/goflux/pkg"
```

The exported API stays compatible where possible; GoFlux keeps the same overall concepts (time series, indicators, strategies, rules) while continuing maintenance and adding new indicators/tests over time.

## Project Layout

GoFlux is currently organized as a single Go package under `pkg/`, with usage examples under `example/`.

## Next Steps

See [BEADS.md](BEADS.md) for the current roadmap and ongoing work.
