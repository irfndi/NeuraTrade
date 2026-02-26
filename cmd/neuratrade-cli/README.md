# NeuraTrade CLI

Native command-line interface to start, stop, and inspect NeuraTrade services.

## Build

```bash
make build
# binary: ./bin/neuratrade
```

## Gateway Commands

```bash
# Start all services
./bin/neuratrade gateway start

# Stop all services
./bin/neuratrade gateway stop

# Check status
./bin/neuratrade gateway status
```

## Other Useful Commands

```bash
./bin/neuratrade status
./bin/neuratrade health
./bin/neuratrade exchanges list
./bin/neuratrade ai models
```

## Runtime Notes

- Service logs and PID files are stored under `NEURATRADE_HOME` (default `~/.neuratrade`).
- `gateway start` launches backend, ccxt, and telegram services as native processes.
- Configure runtime via `.env` and/or `~/.neuratrade/config.json`.
