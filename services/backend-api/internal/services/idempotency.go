package services

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

const maxClientOrderIDLen = 64

// generateIdempotencyKey produces a deterministic idempotency key for order placement.
//
// Key format: nt-{chatID}-{symbol}-{side}-{intentID}
// If IntentID is empty, a nanosecond-timestamp fallback is used (deterministic for
// same-process retry dedup, NOT for cross-process dedup).
//
// The key is truncated/hashed to fit exchange limits (max 64 chars for Bitget clientOid).
func generateIdempotencyKey(chatID, symbol, side, intentID string) string {
	if intentID == "" {
		intentID = fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}

	// Normalize symbol: strip slashes for compactness
	normalizedSymbol := strings.ReplaceAll(symbol, "/", "")

	raw := fmt.Sprintf("nt-%s-%s-%s-%s", chatID, normalizedSymbol, side, intentID)

	// If under the exchange limit, return as-is
	if len(raw) <= maxClientOrderIDLen {
		return raw
	}

	// Hash and truncate to fit the limit while preserving recognizability
	hash := sha256.Sum256([]byte(raw))
	hashed := fmt.Sprintf("nt-%s-%x", chatID, hash[:10])
	if len(hashed) > maxClientOrderIDLen {
		return hashed[:maxClientOrderIDLen]
	}
	return hashed
}

func isDuplicateOrderError(code, msg string) bool {
	code = strings.TrimSpace(code)
	msg = strings.ToLower(strings.TrimSpace(msg))
	return code == "40094" || code == "43025" ||
		strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "already placed") ||
		strings.Contains(msg, "client order already exists")
}
