# Telegram Bot Troubleshooting Guide

This guide helps diagnose and fix issues with the NeuraTrade Telegram bot, particularly when the `/start` command doesn't respond.

## Quick Diagnosis

Run the diagnostic script to automatically check your bot configuration:

```bash
./scripts/diagnose-telegram-bot.sh
```

This script checks:
- Bot token configuration
- Telegram API connectivity
- Webhook configuration
- Database connection
- Application health

## Common Issues and Solutions

### 1. Bot Not Responding to /start Command

**Symptoms:**
- User sends `/start` to the bot
- No response received
- No error messages visible to user

**Possible Causes:**

#### A. Bot Not Initialized
**Check logs for:**
```
[TELEGRAM] ERROR: Failed to create Telegram bot
[TELEGRAM] WARNING: Telegram bot functionality will be disabled
```

**Solution:**
1. Verify `TELEGRAM_BOT_TOKEN` is set in `.env`:
   ```bash
   grep TELEGRAM_BOT_TOKEN .env
   ```
2. Validate token format (should be like `123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11`)

3. Test token with Telegram API:

   > **Security Note:** The following commands will expose your bot token in shell history and process lists. Consider using `set +o history` before running these commands in production, or use a secure secret management tool.

   ```bash
   curl -s "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getMe"
   ```

#### B. Webhook Not Configured
**Check logs for:**
```
[TELEGRAM] Telegram bot configured for webhook mode
[TELEGRAM] Webhook should be set to: https://...
```

**Solution:**
1. Set the webhook URL:
   ```bash
   ./scripts/set-telegram-webhook.sh https://your-domain.com/api/v1/telegram/webhook
   ```
2. Verify webhook is set:
   ```bash
   curl -s "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getWebhookInfo"
   ```
3. Ensure your application is accessible at the webhook URL
4. Webhook URL must use HTTPS (not HTTP)

#### C. Polling Mode Not Working
**Check logs for:**
```
[TELEGRAM] Starting Telegram bot in polling mode
[TELEGRAM] Bot polling started, listening for updates...
```

**Solution:**
1. Set `TELEGRAM_USE_POLLING=true` in config or environment
2. Remove or clear webhook:
   ```bash
   curl -s "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/deleteWebhook?drop_pending_updates=true"
   ```
3. Restart the application

#### D. Database Connection Issues
**Check logs for:**
```
[TELEGRAM] ERROR: Database error checking user
[TELEGRAM] ERROR: Failed to create user
```

**Solution:**
1. Verify database connection in `.env`:
   ```bash
   grep DATABASE_ .env
   ```
2. Test database connectivity:
   ```bash
   psql "$DATABASE_URL" -c "SELECT 1"
   ```
3. Check if `users` table exists:
   ```bash
   psql "$DATABASE_URL" -c "\dt users"
   ```
4. Run migrations if needed:
   ```bash
   make migrate
   ```

#### E. Application Not Running
**Solution:**
1. Check if application is running:
   ```bash
   curl http://localhost:8080/health
   ```
2. Start the application:
   ```bash
   make run
   ```
3. Check application logs for errors

### 2. Webhook Receiving Requests But Not Processing

**Check logs for:**
```
[TELEGRAM] HandleWebhook called from <IP>
[TELEGRAM] Webhook update received: update_id=<id>
[TELEGRAM] processUpdate called
```

**If you see webhook requests but no processing:**
1. Check for database errors in logs
2. Verify bot instance is not nil:
   ```
   [TELEGRAM] ERROR: Telegram bot is not initialized
   ```
3. Check Redis connection for caching features

### 3. Bot Token Invalid or Revoked

**Symptoms:**
```
[TELEGRAM] ERROR: Failed to create Telegram bot: error call getMe
```

**Solution:**
1. Get a new bot token from @BotFather on Telegram:
   - Send `/newbot` to @BotFather
   - Follow prompts to create bot
   - Copy the token
2. Update `.env` with new token:
   ```
   TELEGRAM_BOT_TOKEN=your_new_token_here
   ```
3. Restart application

### 4. Database User Creation Failures

**Check logs for:**
```
[TELEGRAM] ERROR: Failed to create user
```

**Common causes:**
- Duplicate telegram_chat_id (user already exists)
- Missing database permissions
- Invalid database schema

**Solution:**
1. Check existing users:
   ```sql
   SELECT * FROM users WHERE telegram_chat_id = 'CHAT_ID';
   ```
2. Grant necessary permissions:
   ```sql
   GRANT INSERT, UPDATE, SELECT ON users TO neuratrade_user;
   ```
3. Verify schema matches application expectations

## Configuration Modes

### Webhook Mode (Production)
```yaml
telegram:
  bot_token: "YOUR_BOT_TOKEN"
  webhook_url: "https://your-domain.com/api/v1/telegram/webhook"
  use_polling: false
```

**Pros:**
- More efficient for production
- Better for high-volume bots
- No constant polling overhead

**Cons:**
- Requires publicly accessible HTTPS endpoint
- More complex setup

### Polling Mode (Development)
```yaml
telegram:
  bot_token: "YOUR_BOT_TOKEN"
  use_polling: true
  polling_timeout: 60
```

**Pros:**
- Simpler setup
- Works without public URL
- Good for development/testing

**Cons:**
- Less efficient
- Constant polling overhead
- Not recommended for production

## Debugging Steps

### Enable Debug Logging
All Telegram bot operations are logged with `[TELEGRAM]` prefix. Look for:

```bash
# Successful initialization
[TELEGRAM] Bot instance created successfully
[TELEGRAM] Starting Telegram bot in polling mode

# Message processing
[TELEGRAM] Message received from user <id> in chat <id>: /start
[TELEGRAM] handleStartCommand called for user <id>, chat <id>

# Errors
[TELEGRAM] ERROR: <error details>
```

### Test Bot Manually

1. **Test bot info:**
   ```bash
   curl "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getMe"
   ```

2. **Test getting updates:**
   ```bash
   curl "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getUpdates"
   ```

3. **Test sending message:**
   ```bash
   curl -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/sendMessage" \
     -d "chat_id=YOUR_CHAT_ID" \
     -d "text=Test message"
   ```

### Check Application Health

```bash
# Health check
curl http://localhost:8080/health

# Specific Telegram webhook endpoint
curl -X POST http://localhost:8080/api/v1/telegram/webhook \
  -H "Content-Type: application/json" \
  -d '{"message":{"from":{"id":123},"chat":{"id":456},"text":"/start"}}'
```

## Environment Variables

Required environment variables for Telegram bot:

```bash
# Required
TELEGRAM_BOT_TOKEN=your_bot_token_here

# Optional (webhook mode)
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/v1/telegram/webhook

# Optional (polling mode)
TELEGRAM_USE_POLLING=false
TELEGRAM_POLLING_TIMEOUT=60
TELEGRAM_POLLING_LIMIT=100
TELEGRAM_POLLING_OFFSET=0
```

## Testing

Run the test suite:

```bash
# Run all Telegram tests
go test -v ./internal/api/handlers -run TestTelegram

# Run specific test
go test -v ./internal/api/handlers -run TestTelegramHandler_HandleStartCommand

# Run with coverage
go test -cover ./internal/api/handlers -run TestTelegram
```

## Monitoring

### Key Metrics to Monitor

1. **Bot initialization status:**
   - Check logs for successful bot creation
   - Verify bot info can be retrieved

2. **Message processing:**
   - Count of messages received
   - Processing time per message
   - Error rate

3. **Database operations:**
   - User creation success rate
   - Query latency
   - Connection pool status

4. **Webhook status:**
   - Pending updates count
   - Last error time
   - Webhook response time

### Health Check Endpoint

The application provides a health check endpoint:

```bash
curl http://localhost:8080/health
```

Response includes:
- Application status
- Database connectivity
- Redis connectivity
- Timestamp

## Getting Help

If you're still experiencing issues:

1. Check application logs for `[TELEGRAM]` entries
2. Run the diagnostic script: `./scripts/diagnose-telegram-bot.sh`
3. Verify all environment variables are set correctly
4. Test bot token independently with Telegram API
5. Check network connectivity to api.telegram.org
6. Verify database migrations are up to date

## Related Documentation

- [Telegram Bot API Documentation](https://core.telegram.org/bots/api)
- [Go Telegram Bot Library](https://github.com/go-telegram/bot)
- Application README.md for general setup
- `.env.example` for environment variable templates
