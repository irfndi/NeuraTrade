package handlers

import "github.com/irfndi/neuratrade/internal/utils"

func sanitizeCacheKey(input string) string {
	return utils.SanitizeCacheKey(input)
}
