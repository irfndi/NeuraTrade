package services

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

const maxClientOrderIDLen = 64

// ErrMissingIntentID is returned when a caller asks for an idempotency key
// without providing an IntentID. Every order placement must carry an
// IntentID so retries are safely deduped across processes and restarts.
var ErrMissingIntentID = errors.New("idempotency: IntentID is required")

// generateIdempotencyKey produces a deterministic idempotency key for order placement.
//
// Key format: nt-{chatID}-{symbol}-{side}-{intentID}
// IntentID is mandatory; the caller must supply a stable per-decision identifier
// (LLM/strategy decision ID, retry counter, etc.) so retries dedup correctly.
// The key is truncated/hashed to fit exchange limits (max 64 chars for Bitget clientOid).
func generateIdempotencyKey(chatID, symbol, side, intentID string) (string, error) {
	if strings.TrimSpace(intentID) == "" {
		return "", ErrMissingIntentID
	}

	normalizedSymbol := strings.ReplaceAll(symbol, "/", "")

	raw := fmt.Sprintf("nt-%s-%s-%s-%s", chatID, normalizedSymbol, side, intentID)

	if len(raw) <= maxClientOrderIDLen {
		return raw, nil
	}

	hash := sha256.Sum256([]byte(raw))
	hashed := fmt.Sprintf("nt-%s-%x", chatID, hash[:10])
	if len(hashed) > maxClientOrderIDLen {
		return hashed[:maxClientOrderIDLen], nil
	}
	return hashed, nil
}

func isDuplicateOrderError(code, msg string) bool {
	code = strings.TrimSpace(code)
	msg = strings.ToLower(strings.TrimSpace(msg))
	return code == "40094" || code == "43025" ||
		strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "already placed") ||
		strings.Contains(msg, "client order already exists")
}
