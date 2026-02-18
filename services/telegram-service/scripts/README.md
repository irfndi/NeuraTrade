## Security Note

These test scripts use environment variables for sensitive credentials:
- `TELEGRAM_BOT_TOKEN` - Bot token (required)
- `TELEGRAM_CHAT_ID` - Chat ID for testing (defaults to 1082762347)

**Never commit actual credentials!** Export them before running:

```bash
export TELEGRAM_BOT_TOKEN="your-actual-token"
export TELEGRAM_CHAT_ID="your-chat-id"
```

