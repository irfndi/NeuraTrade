# Deployment Guide (Coolify)

This project is designed to be deployed on a VPS managed by [Coolify](https://coolify.io/).
We use a single Docker container architecture that unifies the Go backend and Bun (CCXT) service, while relying on Coolify-managed PostgreSQL and Redis services.

## Architecture

- **Application Container**: A single Docker container running both the Go application (port 8080) and the CCXT service (port 3001).
- **Database**: Managed PostgreSQL service (provisioned via Coolify).
- **Cache**: Managed Redis service (provisioned via Coolify).
- **Routing**: Handled automatically by Coolify's built-in Traefik proxy.

## Optional Split Deploy (Future)

If you later want to deploy services separately, use:
- `Dockerfile.api` for the Go API service
- `ccxt-service/Dockerfile` for the CCXT service
- `telegram-service/Dockerfile` for the Telegram bot service

When split, set `CCXT_SERVICE_URL` to the CCXT container URL (e.g., `http://ccxt-service:3001`).
If you run the Telegram bot as a separate service, set `TELEGRAM_EXTERNAL_SERVICE=true` on the Go API to disable the built-in bot.

## Deployment Steps

### 1. Prepare Coolify Resources

1.  **Create a Project** in Coolify.
2.  **Add a PostgreSQL Service**:
    - Note the connection details (Host, Port, User, Password, Database).
    - Allow external access if you need to run migrations from your local machine (optional, as migrations run automatically on startup).
3.  **Add a Redis Service**:
    - Note the connection details (Host, Port, Password).

### 2. Configure Application Resource

1.  **Add New Resource** -> **GitHub/GitLab Repository**.
2.  Select this repository and branch (e.g., `main`).
3.  **Build Pack**: Select **Docker Compose**.
4.  **Docker Compose File**: Coolify should automatically detect `docker-compose.yml`.
    - Ensure it uses the `docker-compose.yml` (which defines only the `neuratrade` service), NOT `docker-compose.dev.yml`.

### 3. Environment Variables

Configure the following environment variables in Coolify for the application resource:

| Variable | Description | Example / Value |
|----------|-------------|-----------------|
| `DATABASE_HOST` | PostgreSQL Host | `uuid-of-postgres-service` (internal DNS) |
| `DATABASE_PORT` | PostgreSQL Port | `5432` |
| `DATABASE_USER` | PostgreSQL User | `postgres` |
| `DATABASE_PASSWORD` | PostgreSQL Password | `your-db-password` |
| `DATABASE_DBNAME` | PostgreSQL Database Name | `neuratrade` |
| `REDIS_HOST` | Redis Host | `uuid-of-redis-service` (internal DNS) |
| `REDIS_PORT` | Redis Port | `6379` |
| `JWT_SECRET` | Secret for JWT tokens | `generate-secure-random-string` |
| `CCXT_SERVICE_URL` | Internal URL for CCXT | `http://localhost:3001` (Internal loopback) |
| `RUN_MIGRATIONS` | Auto-run DB migrations | `true` |
| `EXTERNAL_CONNECTIONS_ENABLED` | Enable external APIs | `true` |
| `TELEGRAM_WEBHOOK_ENABLED` | Enable Telegram bot | `true` |

**Note:** Use the *internal* network names (usually the container name or UUID provided by Coolify) for `DATABASE_HOST` and `REDIS_HOST` to ensure low latency and security.

### 4. Deploy

1.  Click **Deploy**.
2.  Coolify will:
    - Pull the code.
    - Build the Docker image (using the multi-stage `Dockerfile`).
    - Start the container.
    - The `entrypoint.sh` script will automatically run database migrations.
    - Traefik will automatically route traffic to the exposed ports.

### 5. Domain Configuration

1.  In Coolify, go to the Application settings.
2.  Set the **Domains** (e.g., `https://api.neuratrade.ai`).
3.  Coolify/Traefik will handle SSL termination automatically.

## Local Development

For local development, we use `docker-compose.dev.yml` which includes local PostgreSQL and Redis containers.

```bash
# Start development environment (App + DB + Redis)
make dev-up-orchestrated

# Stop development environment
make dev-down

# Run migrations locally
make db-migrate
```

## Troubleshooting

- **Migration Failures**: Check the deployment logs. Ensure `DATABASE_` env vars are correct.
- **Service Communication**: The Go app communicates with the CCXT service via `localhost:3001` inside the container. Ensure `CCXT_SERVICE_URL` is set to `http://localhost:3001`.
- **Health check stuck as unhealthy**: The Docker health check hits `http://localhost:8080/health` from inside the container. A 503 is usually caused by missing critical env vars (e.g., `TELEGRAM_BOT_TOKEN`), a down CCXT service, or stale env after manual edits.
- **Coolify DNS / host-name issues**: If you see `could not translate host name` for Postgres/Redis, make sure the app service is attached to the Coolify network and that `DATABASE_HOST` / `REDIS_HOST` use the internal service names Coolify provides (often UUID-like container names).
- **Applying env changes**: After editing `.env` or Coolify env vars, recreate the container so the new values are loaded: `docker compose up -d --force-recreate neuratrade` in the application directory. Avoid leaving files immutable (`chattr +i`) unless you are intentionally freezing them; unlock with `chattr -i .env docker-compose.yaml` before updates and relock if needed.
- **Numeric env parsing failures**: Values like `DATABASE_MAX_OPEN_CONNS`, `DATABASE_MAX_IDLE_CONNS`, timeouts, and CCXT/Sentry samples must be plain numbers without quotes (e.g., `DATABASE_MAX_OPEN_CONNS=25`, `DATABASE_STATEMENT_TIMEOUT=300000`, `SENTRY_TRACES_SAMPLE_RATE=0.2`). Empty strings or `${VAR}` placeholders will fail to parse at boot.
- **PostgreSQL query_timeout error**: If you see `FATAL: unrecognized configuration parameter "query_timeout"`, this is now fixed. PostgreSQL uses `statement_timeout` instead. The config now automatically uses the correct parameter.
- **CCXT service port**: The Bun CCXT server listens on `PORT` (default 3001) inside the container. Keep `CCXT_SERVICE_URL` aligned (e.g., `http://127.0.0.1:3001` or the port you override) to avoid connection refused errors during startup or cache warming.
- **Coolify redeploys overwrite compose/env**: Webhook deploys rewrite `.env` and `docker-compose.yaml`. Make sure the Coolify UI holds the correct values (DB/Redis hosts, `CCXT_SERVICE_URL`, numeric DB knobs, Telegram token). Add an override in Coolify to mount the CCXT dist fix if you use it: `/data/coolify/applications/<app-uuid>/ccxt-dist/index.js:/app/ccxt/dist/index.js:ro`, and keep the service on both networks (`coolify` plus the app network) so DB/Redis resolve.
- **Bun double-serve (EADDRINUSE)**: If CCXT logs `Failed to start server. Is port in use?`, the entrypoint now automatically kills stale processes on the configured port. If issues persist, set a different `PORT` environment variable (e.g., `PORT=3001`) or use `CCXT_AUTO_SERVE=false` to disable auto-serve.

## Coolify-specific stability checklist

- Use `docker-compose.yaml` from the repo (it now attaches to the shared `coolify` network by default via `COOLIFY_SHARED_NETWORK`, default `coolify`) so Postgres/Redis hostnames resolve on redeploy.
- Keep Coolify env vars aligned with runtime: DB/Redis hosts/passwords, `CCXT_SERVICE_URL=http://127.0.0.1:3001`, `PORT=3001`, `BUN_NO_SERVER=false` (or unset), and the numeric DB tuning knobs (`DATABASE_MAX_OPEN_CONNS`, `DATABASE_MAX_IDLE_CONNS`, timeouts, etc.) as numbers only.
- Set `COOLIFY_SHARED_NETWORK=coolify` in Coolify env if your shared network has a custom name; the compose file will attach to it automatically.
- If you customize CCXT dist, add a persistent volume override in Coolify, otherwise rely on the bundled code (preferred to avoid overrides being wiped on redeploy).
- After updating env/compose in Coolify, trigger a redeploy (webhook is fine) and confirm `docker ps` shows the container on the `coolify` network and `curl http://localhost:8080/health` is healthy inside the container.
