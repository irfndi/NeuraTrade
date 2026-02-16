# NeuraTrade Bot Command Test Plan

## Phase 1: Basic Commands (No Setup Required)

### 1. Start & Help
```
/start
/help
```
**Expected:** Welcome message and help text

### 2. AI Commands
```
/ai_models
/ai_status
/ai_route fast
/ai_route balanced
/ai_route accurate
/ai_select gpt-4o-mini
```
**Expected:** AI model list, status, routing results

### 3. System Status
```
/status
/opportunities
/settings
```
**Expected:** System info, trading opportunities, settings

## Phase 2: Wallet & Exchange Setup (Required for Trading)

### 4. Wallet Commands
```
/wallet
/add_wallet <address>
/remove_wallet <address>
/connect_exchange binance
/connect_polymarket
```
**Expected:** Wallet management (requires PostgreSQL for full functionality)

## Phase 3: Trading Commands (Requires Setup)

### 5. Portfolio & Performance
```
/portfolio
/performance
/summary
```
**Expected:** Trading data (requires wallet setup)

### 6. Autonomous Mode
```
/begin
/quests
/pause
```
**Expected:** Start/pause autonomous trading

### 7. Trading Actions
```
/liquidate <symbol>
/liquidate_all
```
**Expected:** Execute trades (requires exchange connection)

## Phase 4: Monitoring & Diagnostics

### 8. Monitoring
```
/doctor
/logs
/alerts
```
**Expected:** System diagnostics and logs

## Current Status

**Working in SQLite Mode:**
- ✅ /start, /help
- ✅ /ai_models, /ai_status, /ai_route
- ✅ /status, /opportunities
- ✅ /settings

**Requires PostgreSQL + Wallet Setup:**
- ❌ /wallet, /add_wallet, /remove_wallet
- ❌ /portfolio, /performance, /summary
- ❌ /begin, /quests, /liquidate
- ❌ /doctor (partial)

## To Test Autonomous Trading Stream

You need:
1. **Switch to PostgreSQL mode** (not SQLite)
2. **Add exchange API keys:**
   ```
   /connect_exchange binance
   ```
3. **Add trading wallet:**
   ```
   /add_wallet <your_wallet_address>
   ```
4. **Start autonomous mode:**
   ```
   /begin
   ```

**What you'll see in stream:**
- Trade signals detected
- Orders being placed
- Position updates
- Profit/loss reports
- Risk management alerts

## Test Script

Run this to test all working commands:

```bash
cd /Users/irfandi/Coding/2025/NeuraTrade/services/telegram-service
bash scripts/test-commands-live.sh
```

This will send all commands and capture responses.
