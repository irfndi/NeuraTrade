# Dynamic Exchange Management

## Overview

NeuraTrade now supports **dynamic, user-configurable exchange management**. Instead of loading all 100+ CCXT exchanges (which is inefficient and bloated), the system only loads exchanges that you explicitly configure.

## Benefits

- ✅ **Efficient**: Only runs necessary exchange connections
- ✅ **Non-bloated**: No wasted resources on unused exchanges
- ✅ **User-controlled**: You decide which exchanges to use
- ✅ **Configurable authentication**: Optional API keys per exchange for private data
- ✅ **Dynamic**: Add/remove exchanges without restarting services

## Usage

### List Configured Exchanges

```bash
neuratrade exchanges list
```

**Output:**
```
Configured Exchanges
====================

Found 3 configured exchanges:

  ✓ binance [active]
  🔑 ✓ bybit [active]
  ✓ okx [active]

Legend:
  🔑 = Has API credentials (private data access)
  ✓  = Active and loading market data
  ⚠️  = Configured but disabled
```

### Add an Exchange (Public Market Data Only)

```bash
neuratrade exchanges add --name binance
```

This enables:
- ✅ Public tickers
- ✅ Order books
- ✅ OHLCV data
- ✅ Funding rates (for futures)

### Add an Exchange (With Authentication)

```bash
neuratrade exchanges add --name bybit \
  --api-key YOUR_API_KEY \
  --secret YOUR_API_SECRET
```

This additionally enables:
- ✅ Private order data
- ✅ Account balances
- ✅ Order placement/cancellation
- ✅ Position management

⚠️ **Security Note**: API credentials are stored in `~/.neuratrade/config.json` with file permissions `0600` (read/write for owner only).

### Remove an Exchange

```bash
neuratrade exchanges remove --name binance
```

This:
- Removes the exchange from active connections
- Deletes associated API keys (if any)
- Stops market data collection for that exchange

### Reload Exchange Configuration

```bash
neuratrade exchanges reload
```

Applies configuration changes without restarting the CCXT service.

## Configuration File

Exchange configurations are stored in `~/.neuratrade/config.json`:

```json
{
  "exchanges": {
    "enabled": ["binance", "bybit", "okx"],
    "api_keys": {
      "bybit": {
        "apiKey": "YOUR_API_KEY",
        "secret": "YOUR_API_SECRET"
      }
    }
  },
  "market_data": {
    "max_age_minutes": 10,
    "cleanup_interval_minutes": 5
  }
}
```

## Workflow Example

### Scenario 1: First-Time Setup

```bash
# Start with default exchanges (binance, bybit, okx, etc.)
neuratrade gateway start

# Check what's running
neuratrade exchanges list

# Add more exchanges as needed
neuratrade exchanges add --name kraken
neuratrade exchanges add --name gateio

# Reload to apply changes
neuratrade exchanges reload
```

### Scenario 2: Adding Private Trading

```bash
# Add exchange with API credentials
neuratrade exchanges add --name binance \
  --api-key YOUR_BINANCE_KEY \
  --secret YOUR_BINANCE_SECRET

# Verify configuration
neuratrade exchanges list
# Should show: 🔑 ✓ binance [active]

# Now you can use private endpoints:
# - Place orders via /api/order
# - Check balances
# - View open/closed orders
```

### Scenario 3: Cleaning Up

```bash
# Remove unused exchanges to save resources
neuratrade exchanges remove --name kraken
neuratrade exchanges remove --name gateio

# Verify
neuratrade exchanges list
```

## API Endpoints

### Get Configured Exchanges

```bash
GET /api/v1/exchanges
Authorization: Bearer <ADMIN_API_KEY>
```

**Response:**
```json
{
  "exchanges": [
    {
      "name": "binance",
      "enabled": true,
      "has_auth": false,
      "added_at": "2024-02-17T10:30:00Z"
    },
    {
      "name": "bybit",
      "enabled": true,
      "has_auth": true,
      "added_at": "2024-02-17T10:35:00Z"
    }
  ],
  "count": 2
}
```

### Add Exchange

```bash
POST /api/v1/exchanges
Authorization: Bearer <ADMIN_API_KEY>
Content-Type: application/json

{
  "name": "okx",
  "api_key": "YOUR_OKX_KEY",
  "secret": "YOUR_OKX_SECRET"
}
```

### Remove Exchange

```bash
DELETE /api/v1/exchanges
Authorization: Bearer <ADMIN_API_KEY>
Content-Type: application/json

{
  "name": "okx"
}
```

### Reload Configuration

```bash
POST /api/v1/exchanges/reload
Authorization: Bearer <ADMIN_API_KEY>
```

## Troubleshooting

### Exchange Failed to Initialize

```
⚠️ Failed to initialize exchange ftx: Exchange class not found
```

**Solution**: The exchange may be:
- Blacklisted (defunct/unreliable)
- Not supported by CCXT
- Temporarily unavailable

Check the list of supported exchanges: `ccxt.exchanges`

### API Credentials Not Working

```
✗ Authentication failed for bybit
```

**Solutions**:
1. Verify API key/secret are correct
2. Ensure API key has required permissions
3. Check if exchange requires IP whitelisting
4. Remove and re-add the exchange with correct credentials

### Configuration Not Persisting

```
⚠️ Warning: Could not save exchange configuration
```

**Solutions**:
1. Check file permissions: `ls -la ~/.neuratrade/config.json`
2. Ensure directory exists: `mkdir -p ~/.neuratrade`
3. Verify disk space

## Best Practices

1. **Start Minimal**: Only add exchanges you actively use
2. **Use API Keys Carefully**: Only provide credentials when needed for private data
3. **Regular Cleanup**: Remove unused exchanges periodically
4. **Monitor Resources**: Check `neuratrade exchanges list` to see active connections
5. **Secure Credentials**: Never commit `~/.neuratrade/config.json` to version control

## Migration from Old Setup

If you were using the previous version with hardcoded exchanges:

```bash
# 1. Stop services
neuratrade gateway stop

# 2. Create new config with your preferred exchanges
cat > ~/.neuratrade/config.json <<EOF
{
  "exchanges": {
    "enabled": ["binance", "bybit"],
    "api_keys": {}
  }
}
EOF

# 3. Start services
neuratrade gateway start

# 4. Verify
neuratrade exchanges list
```

## Support

For issues or questions:
- Check logs: `tail -f ~/.neuratrade/logs/ccxt.log`
- View documentation: `docs/`
- Run health check: `neuratrade health`
