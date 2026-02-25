package services

import "strings"

// isBlacklistableError checks if an error indicates a symbol should be blacklisted
func isBlacklistableError(err error) (bool, string) {
	if err == nil {
		return false, ""
	}

	errorMsg := err.Error()

	// Coinbase delisted products
	if strings.Contains(errorMsg, "Not allowed for delisted products") {
		return true, "coinbase_delisted"
	}

	// Binance missing market symbols
	if strings.Contains(errorMsg, "does not have market symbol") {
		return true, "binance_missing_symbol"
	}

	// General delisted indicators
	if strings.Contains(errorMsg, "delisted") {
		return true, "delisted"
	}

	// Inactive symbols
	if strings.Contains(errorMsg, "inactive") {
		return true, "inactive"
	}

	// Symbol not found or unavailable
	if strings.Contains(errorMsg, "symbol not found") ||
		strings.Contains(errorMsg, "symbol unavailable") ||
		strings.Contains(errorMsg, "market not found") {
		return true, "symbol_not_found"
	}

	// Exchange-specific error patterns
	if strings.Contains(errorMsg, "CCXT service error (500)") {
		// Check for specific 500 error patterns that indicate permanent issues
		if strings.Contains(errorMsg, "Not allowed") ||
			strings.Contains(errorMsg, "does not have") ||
			strings.Contains(errorMsg, "delisted") {
			return true, "ccxt_500_permanent"
		}
	}

	return false, ""
}

// isFundingRateUnsupportedError checks if an error indicates the exchange doesn't support funding rates
func isFundingRateUnsupportedError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := strings.ToLower(err.Error())

	// Check for funding rate unsupported error patterns
	return strings.Contains(errorMsg, "does not support funding rates") ||
		strings.Contains(errorMsg, "funding rates not supported") ||
		strings.Contains(errorMsg, "no funding rates") ||
		(strings.Contains(errorMsg, "ccxt service error (400)") && strings.Contains(errorMsg, "funding"))
}

// isValidSymbolFormat validates that a symbol has a valid format before processing.
// This filters out malformed symbols like ":" that some exchanges return.
func isValidSymbolFormat(symbol string) bool {
	// Reject empty or whitespace-only symbols
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return false
	}

	// Reject symbols that are just separators (no actual currency data)
	if symbol == ":" || symbol == "/" || symbol == "_" || symbol == "-" {
		return false
	}

	// Check if symbol contains a valid separator with content on both sides
	separators := []string{"/", ":", "_", "-"}
	for _, sep := range separators {
		if strings.Contains(symbol, sep) {
			parts := strings.SplitN(symbol, sep, 2)
			if len(parts) == 2 {
				// Both parts must have content (not just whitespace)
				base := strings.TrimSpace(parts[0])
				quote := strings.TrimSpace(parts[1])
				if len(base) > 0 && len(quote) > 0 {
					return true
				}
				// If separator exists but parts are empty, it's invalid
				return false
			}
		}
	}

	// Allow symbols without separators (some exchanges use concatenated format like BTCUSDT)
	// Must be at least 3 characters (minimum viable symbol length)
	return len(symbol) >= 3
}

// parseSymbol parses a trading symbol into base and quote currencies
// Improved version with more robust parsing logic to handle various symbol formats
func (c *CollectorService) parseSymbol(symbol string) (string, string) {
	// Handle common separators
	if strings.Contains(symbol, "/") {
		parts := strings.Split(symbol, "/")
		if len(parts) >= 2 {
			base := parts[0]
			quote := strings.Split(parts[1], ":")[0] // Remove settlement currency if present
			return base, quote
		}
	}

	// Handle symbols with underscores (some exchanges use this format)
	if strings.Contains(symbol, "_") {
		parts := strings.Split(symbol, "_")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	}

	// Handle symbols with dashes (some exchanges use this format)
	if strings.Contains(symbol, "-") && !c.isOptionsContract(symbol) {
		parts := strings.Split(symbol, "-")
		if len(parts) >= 2 {
			return parts[0], parts[1]
		}
	}

	// Handle symbols without separators (like BTCUSDT)
	// Order matters: longer quotes first to avoid incorrect parsing
	commonQuotes := []string{
		"USDT", "USDC", "BUSD", "TUSD", "FDUSD", // Stablecoins
		"BTC", "ETH", "BNB", "ADA", "DOT", "SOL", // Major cryptos
		"USD", "EUR", "GBP", "JPY", "AUD", "CAD", // Fiat currencies
		"DOGE", "SHIB", "MATIC", "AVAX", "LINK", // Other popular cryptos
	}

	for _, quote := range commonQuotes {
		if strings.HasSuffix(symbol, quote) {
			base := strings.TrimSuffix(symbol, quote)
			// Ensure base currency is not empty and reasonable length
			if len(base) > 0 && len(base) <= 20 {
				return base, quote
			}
		}
	}

	// Last resort: if no pattern matches, return empty strings
	// This prevents incorrect parsing that could cause data corruption
	return "", ""
}

// filterValidSymbols filters out invalid symbols that cause ticker fetch errors
func (c *CollectorService) filterValidSymbols(symbols []string) []string {
	var validSymbols []string

	for _, symbol := range symbols {
		// Skip options contracts (contain dates and strike prices)
		if c.isOptionsContract(symbol) {
			continue
		}

		// Skip symbols with unusual formats that typically cause errors
		if c.isInvalidSymbolFormat(symbol) {
			continue
		}

		validSymbols = append(validSymbols, symbol)
	}

	return validSymbols
}

// isOptionsContract checks if a symbol represents an options contract
func (c *CollectorService) isOptionsContract(symbol string) bool {
	// Options contracts typically have dates and strike prices
	// Examples: SOLUSDT:USDT-250815-180-C, BTC-25DEC20-20000-C
	return strings.Contains(symbol, "-C") || strings.Contains(symbol, "-P") ||
		(strings.Contains(symbol, "-") && (strings.Contains(symbol, "20") || strings.Contains(symbol, "25")))
}

// isInvalidSymbolFormat checks for other invalid symbol formats
func (c *CollectorService) isInvalidSymbolFormat(symbol string) bool {
	// Skip symbols that are too long (align with database VARCHAR(50) limit)
	// Increased from 20 to 50 to match database schema and handle longer derivative symbols
	if len(symbol) > 50 {
		return true
	}

	// Skip symbols with multiple colons (complex derivatives)
	if strings.Count(symbol, ":") > 1 {
		return true
	}

	// Skip symbols with both underscores and dashes (likely complex derivatives)
	// Note: We now handle single underscore or dash as valid separators
	if strings.Contains(symbol, "_") && strings.Contains(symbol, "-") {
		return true
	}

	return false
}
