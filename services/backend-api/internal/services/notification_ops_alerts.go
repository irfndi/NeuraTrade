package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	userModels "github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
)

func loadTelegramMaxMessageUnits() int {
	if v := os.Getenv("TELEGRAM_MAX_MESSAGE_UNITS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return 3900
}

// NotifyQuestProgress sends a quest progress notification to a user
func (ns *NotificationService) NotifyQuestProgress(ctx context.Context, chatID int64, progress QuestProgressNotification) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyQuestProgress", map[string]string{
		"chat_id":  fmt.Sprintf("%d", chatID),
		"quest_id": progress.QuestID,
	})
	defer observability.FinishSpan(span, nil)

	message := ns.formatQuestProgressMessage(progress)

	if err := ns.sendTelegramMessage(spanCtx, chatID, message); err != nil {
		ns.logger.Error("Failed to send quest progress notification",
			"chat_id", chatID,
			"quest_id", progress.QuestID,
			"error", err,
		)
		return err
	}

	ns.logger.Info("Sent quest progress notification",
		"chat_id", chatID,
		"quest_id", progress.QuestID,
		"percent", progress.Percent,
	)

	return nil
}

// formatQuestProgressMessage formats a quest progress notification message
func (ns *NotificationService) formatQuestProgressMessage(progress QuestProgressNotification) string {
	var statusEmoji string
	switch progress.Status {
	case "completed":
		statusEmoji = "✅"
	case "failed":
		statusEmoji = "❌"
	case "expired":
		statusEmoji = "⏰"
	default:
		statusEmoji = "🎯"
	}

	lines := []string{
		fmt.Sprintf("%s Quest Progress Update", statusEmoji),
		"",
		progress.QuestName,
		fmt.Sprintf("Progress: %d/%d (%d%%)", progress.Current, progress.Target, progress.Percent),
	}

	if progress.Status == "completed" {
		lines = append(lines, "", "🎉 Quest completed!")
	} else if progress.TimeRemaining != "" {
		lines = append(lines, fmt.Sprintf("Time remaining: %s", progress.TimeRemaining))
	}

	progressBar := ns.generateProgressBar(progress.Percent, 10)
	lines = append(lines, "", progressBar)

	return joinNotificationLines(lines)
}

// NotifyRiskEvent sends a risk event notification to a user
func (ns *NotificationService) NotifyRiskEvent(ctx context.Context, chatID int64, event RiskEventNotification) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyRiskEvent", map[string]string{
		"chat_id":    fmt.Sprintf("%d", chatID),
		"event_type": event.EventType,
		"severity":   event.Severity,
	})
	defer observability.FinishSpan(span, nil)

	message := ns.formatRiskEventMessage(event)

	if err := ns.sendTelegramMessage(spanCtx, chatID, message); err != nil {
		ns.logger.Error("Failed to send risk event notification",
			"chat_id", chatID,
			"event_type", event.EventType,
			"error", err,
		)
		return err
	}

	ns.logger.Info("Sent risk event notification",
		"chat_id", chatID,
		"event_type", event.EventType,
		"severity", event.Severity,
	)

	return nil
}

// formatRiskEventMessage formats a risk event notification message
func (ns *NotificationService) formatRiskEventMessage(event RiskEventNotification) string {
	var severityEmoji string
	switch event.Severity {
	case "critical":
		severityEmoji = "🚨"
	case "high":
		severityEmoji = "⚠️"
	case "medium":
		severityEmoji = "⚡"
	case "low":
		severityEmoji = "ℹ️"
	default:
		severityEmoji = "⚠️"
	}

	lines := []string{
		fmt.Sprintf("%s Risk Event Alert", severityEmoji),
		"",
		fmt.Sprintf("Type: %s", event.EventType),
		fmt.Sprintf("Severity: %s", event.Severity),
		"",
		event.Message,
	}

	if len(event.Details) > 0 {
		lines = append(lines, "", "Details:")
		keys := make([]string, 0, len(event.Details))
		for key := range event.Details {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("• %s: %s", key, event.Details[key]))
		}
	}

	lines = append(lines, "", fmt.Sprintf("Time: %s", time.Now().UTC().Format(time.RFC3339)))

	return joinNotificationLines(lines)
}

// NotifyFundMilestone sends a fund milestone notification to a user
func (ns *NotificationService) NotifyFundMilestone(ctx context.Context, chatID int64, milestone FundMilestoneNotification) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyFundMilestone", map[string]string{
		"chat_id":        fmt.Sprintf("%d", chatID),
		"milestone_type": milestone.MilestoneType,
	})
	defer observability.FinishSpan(span, nil)

	message := ns.formatFundMilestoneMessage(milestone)

	if err := ns.sendTelegramMessage(spanCtx, chatID, message); err != nil {
		ns.logger.Error("Failed to send fund milestone notification",
			"chat_id", chatID,
			"milestone_type", milestone.MilestoneType,
			"error", err,
		)
		return err
	}

	ns.logger.Info("Sent fund milestone notification",
		"chat_id", chatID,
		"milestone_type", milestone.MilestoneType,
		"percent", milestone.PercentReached,
	)

	return nil
}

// formatFundMilestoneMessage formats a fund milestone notification message
func (ns *NotificationService) formatFundMilestoneMessage(milestone FundMilestoneNotification) string {
	lines := []string{
		"💰 Fund Milestone Reached!",
		"",
		milestone.Achievement,
		"",
		fmt.Sprintf("Current: %s", milestone.CurrentValue),
		fmt.Sprintf("Target: %s", milestone.TargetValue),
		fmt.Sprintf("Progress: %d%%", milestone.PercentReached),
	}

	progressBar := ns.generateProgressBar(milestone.PercentReached, 20)
	lines = append(lines, "", progressBar)

	return joinNotificationLines(lines)
}

// NotifyAIReasoning sends an AI reasoning notification to a user
func (ns *NotificationService) NotifyAIReasoning(ctx context.Context, chatID int64, reasoning AIReasoningNotification) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyAIReasoning", map[string]string{
		"chat_id":       fmt.Sprintf("%d", chatID),
		"decision_type": reasoning.DecisionType,
	})
	defer observability.FinishSpan(span, nil)

	if ns.aiReasoningThrottle != nil {
		key := aiReasoningThrottleKey(chatID, reasoning)
		if key != "" {
			if !ns.aiReasoningThrottle.ShouldSend(key) {
				ns.logger.Debug(
					"AI reasoning notification throttled",
					"chat_id", chatID,
					"decision_type", reasoning.DecisionType,
					"summary", reasoning.Summary,
				)
				return nil
			}
		}
	}

	message := ns.formatAIReasoningMessage(reasoning)

	if err := ns.sendTelegramMessage(spanCtx, chatID, message); err != nil {
		ns.logger.Error("Failed to send AI reasoning notification",
			"chat_id", chatID,
			"decision_type", reasoning.DecisionType,
			"error", err,
		)
		return err
	}

	ns.logger.Info("Sent AI reasoning notification",
		"chat_id", chatID,
		"decision_type", reasoning.DecisionType,
		"confidence", reasoning.Confidence,
	)

	return nil
}

func (ns *NotificationService) formatAIReasoningMessage(reasoning AIReasoningNotification) string {
	confidenceKnown := reasoning.ConfidenceKnown
	if !confidenceKnown &&
		strings.TrimSpace(reasoning.ReasonCategory) == "" &&
		strings.TrimSpace(reasoning.HoldCategory) == "" &&
		!isInfrastructureRuntimeStatus(reasoning.RuntimeStatus) {
		confidenceKnown = true
	}
	category := strings.TrimSpace(reasoning.ReasonCategory)
	if category == "" {
		category = strings.TrimSpace(reasoning.HoldCategory)
	}
	if reasoning.RuntimeStatus != "" && isInfrastructureRuntimeStatus(reasoning.RuntimeStatus) {
		if category == "" {
			category = "execution_unavailable"
		}
	} else {
		if !confidenceKnown && (category == "" || strings.EqualFold(category, "strategy_hold")) {
			evidence := strings.TrimSpace(reasoning.Summary + " " + strings.Join(reasoning.Reasons, " "))
			category = classifyAIRuntimeReason(evidence, "execution_unavailable")
		}
		if isRuntimeReasonCategory(category) {
			confidenceKnown = false
		}
		if !confidenceKnown && strings.EqualFold(category, "strategy_hold") {
			category = "execution_unavailable"
		}
	}

	reasonsToShow := len(reasoning.Reasons)
	lines := buildAIReasoningMessageLines(reasoning, category, confidenceKnown, reasonsToShow)
	mostCompactLines := lines

	message := formatNotificationCodeBlock(lines)
	if telegramMessageUnits(message) <= ns.telegramMaxMessageUnits {
		return message
	}

	for reasonsToShow >= 0 {
		candidateLines := buildAIReasoningMessageLines(reasoning, category, confidenceKnown, reasonsToShow)
		mostCompactLines = candidateLines
		message = formatNotificationCodeBlock(candidateLines)
		if telegramMessageUnits(message) <= ns.telegramMaxMessageUnits {
			return message
		}

		reasonsToShow--
	}

	if reasoning.Action != "" {
		bodyLines, actionTail := splitAIReasoningActionTail(mostCompactLines)
		return formatNotificationCodeBlockWithTailPriority(bodyLines, actionTail, ns.telegramMaxMessageUnits)
	}

	return formatNotificationCodeBlockWithLimit(mostCompactLines, ns.telegramMaxMessageUnits)
}

// buildAIReasoningMessageLines builds a slice of plain-text lines representing an AI reasoning notification.
//
// The produced lines include a header ("🤖 AI Trading Decision"), the decision type, a confidence line
// (an emoji and percentage when `confidenceKnown` is true, otherwise an "N/A (runtime-degraded)" note),
// a summary, and optional fields: reason category, unblock condition, and attempt window progress.
// It appends a "Key Factors" section generated from `reasoning.Reasons`, showing up to `maxReasons` items
// (omitted factors are represented with a summary line). If `reasoning.Action` is non-empty, a
// "Recommended Action" line is appended.
//
// Returns the assembled slice of message lines ready for joining into a notification body.
func buildAIReasoningMessageLines(reasoning AIReasoningNotification, category string, confidenceKnown bool, maxReasons int) []string {
	lines := []string{
		"🤖 AI Trading Decision",
		"",
		fmt.Sprintf("Type: %s", reasoning.DecisionType),
	}

	infraHold := isInfrastructureRuntimeStatus(reasoning.RuntimeStatus)
	llmDegraded := reasoning.RuntimeStatus == runtimeStatusLLMDegraded
	switch {
	case confidenceKnown && infraHold:
		confidencePercent := int(reasoning.Confidence * 100)
		lines = append(lines, fmt.Sprintf("Confidence: 🟡 %d%% (gated)", confidencePercent))
	case infraHold && !confidenceKnown:
		lines = append(lines, "Confidence: ⏸️ (infrastructure hold)")
	case llmDegraded:
		lines = append(lines, "Confidence: ⚪ N/A (runtime-degraded)")
	case confidenceKnown:
		confidencePercent := int(reasoning.Confidence * 100)
		var confidenceEmoji string
		switch {
		case reasoning.Confidence >= 0.8:
			confidenceEmoji = "🟢"
		case reasoning.Confidence >= 0.6:
			confidenceEmoji = "🟡"
		default:
			confidenceEmoji = "🔴"
		}
		lines = append(lines, fmt.Sprintf("Confidence: %s %d%%", confidenceEmoji, confidencePercent))
	default:
		lines = append(lines, "Confidence: ⚪ N/A (runtime-degraded)")
	}

	lines = append(lines,
		"",
		fmt.Sprintf("Summary: %s", reasoning.Summary),
	)
	if category != "" {
		lines = append(lines, fmt.Sprintf("Reason Category: %s", category))
	}
	if reasoning.RuntimeStatus != "" {
		var statusEmoji string
		switch reasoning.RuntimeStatus {
		case runtimeStatusStateDrift, runtimeStatusReconcileBlocked:
			statusEmoji = "🔄"
		case runtimeStatusCircuitOpen:
			statusEmoji = "⚠️"
		case runtimeStatusLLMDegraded:
			statusEmoji = "🛑"
		default:
			statusEmoji = "ℹ️"
		}
		lines = append(lines, fmt.Sprintf("Runtime Status: %s %s", statusEmoji, reasoning.RuntimeStatus))
	}
	if reasoning.RuntimeDetails != nil {
		if current, ok := reasoning.RuntimeDetails["clean_passes_current"]; ok {
			if required, rok := reasoning.RuntimeDetails["clean_passes_required"]; rok {
				lines = append(lines, fmt.Sprintf("Reconcile Progress: %s/%s clean passes", current, required))
			}
		}
		if driftPos, ok := reasoning.RuntimeDetails["drift_positions"]; ok {
			lines = append(lines, fmt.Sprintf("Drift Positions: %s stale", driftPos))
		}
		if recoveryAction, ok := reasoning.RuntimeDetails["recovery_action"]; ok {
			lines = append(lines, fmt.Sprintf("Recovery Action: %s", recoveryAction))
		}
		if total, ok := reasoning.RuntimeDetails["ai_window_total"]; ok {
			if errors, eok := reasoning.RuntimeDetails["ai_window_errors"]; eok {
				if rate, rok := reasoning.RuntimeDetails["ai_error_rate"]; rok {
					lines = append(lines, fmt.Sprintf("AI Runtime Window: %s/%s errors (%s)", errors, total, rate))
				} else {
					lines = append(lines, fmt.Sprintf("AI Runtime Window: %s/%s errors", errors, total))
				}
			}
		}
		if attempts, ok := reasoning.RuntimeDetails["ai_failover_attempts"]; ok {
			if failures, fok := reasoning.RuntimeDetails["ai_failover_failures"]; fok {
				lines = append(lines, fmt.Sprintf("AI Failover: %s attempts, %s failures", attempts, failures))
			} else {
				lines = append(lines, fmt.Sprintf("AI Failover: %s attempts", attempts))
			}
		}
		if failedProviders, ok := reasoning.RuntimeDetails["ai_failed_providers"]; ok {
			lines = append(lines, fmt.Sprintf("AI Failed Providers: %s", failedProviders))
		}
		if category, ok := reasoning.RuntimeDetails["ai_last_category"]; ok {
			lines = append(lines, fmt.Sprintf("AI Last Category: %s", category))
		}
		if lastError, ok := reasoning.RuntimeDetails["ai_last_error"]; ok {
			lines = append(lines, fmt.Sprintf("AI Last Error: %s", lastError))
		}
	}
	if strings.TrimSpace(reasoning.UnblockCondition) != "" {
		lines = append(lines, fmt.Sprintf("Unblock Condition: %s", strings.TrimSpace(reasoning.UnblockCondition)))
	}
	if strings.TrimSpace(reasoning.AttemptWindowProgress) != "" {
		lines = append(lines, fmt.Sprintf("Attempt Window: %s", strings.TrimSpace(reasoning.AttemptWindowProgress)))
	}

	lines = append(lines, buildAIReasoningFactorLines(reasoning.Reasons, maxReasons)...)
	if reasoning.Action != "" {
		lines = append(lines, "", fmt.Sprintf("Recommended Action: %s", reasoning.Action))
	}

	return lines
}

// buildAIReasoningFactorLines builds lines describing key factors from the provided reasons.
// It returns nil when there are no reasons. The output begins with a blank line and the
// header "Key Factors:" followed by up to maxReasons bullet lines (one per reason).
// If some reasons are omitted, a trailing bullet indicates how many factors were omitted;
// when no reasons are shown (maxReasons <= 0) the omission line explains they were
// omitted due to message length.
func buildAIReasoningFactorLines(reasons []string, maxReasons int) []string {
	if len(reasons) == 0 {
		return nil
	}

	limit := len(reasons)
	if maxReasons < 0 {
		limit = 0
	} else if maxReasons < limit {
		limit = maxReasons
	}

	factorLines := []string{"", "Key Factors:"}
	for i := 0; i < limit; i++ {
		factorLines = append(factorLines, fmt.Sprintf("• %s", reasons[i]))
	}

	omitted := len(reasons) - limit
	if omitted > 0 {
		if limit > 0 {
			factorLines = append(factorLines, fmt.Sprintf("• ... and %d more factors", omitted))
		} else {
			factorLines = append(factorLines, fmt.Sprintf("• %d factors omitted due to message length", omitted))
		}
	}

	return factorLines
}

func splitAIReasoningActionTail(lines []string) ([]string, []string) {
	if len(lines) < 2 {
		return lines, nil
	}
	return lines[:len(lines)-2], lines[len(lines)-2:]
}

// formatNotificationCodeBlock joins the provided lines into a single notification message separated by newlines.
func formatNotificationCodeBlock(lines []string) string {
	return joinNotificationLines(lines)
}

// formatNotificationCodeBlockWithLimit formats the provided lines into a single notification message and ensures the result does not exceed maxUnits Telegram message units.
// If the formatted message fits within maxUnits it is returned unchanged; otherwise the message is truncated to maxUnits and an ellipsis ("...") is appended.
func formatNotificationCodeBlockWithLimit(lines []string, maxUnits int) string {
	message := formatNotificationCodeBlock(lines)
	if telegramMessageUnits(message) <= maxUnits {
		return message
	}

	truncated := truncateToTelegramUnitsWithEllipsis(joinNotificationLines(lines), maxUnits)

	return truncated
}

// formatNotificationCodeBlockWithTailPriority prepares a notification message that prioritizes the tail content when enforcing a maximum telegram unit limit.
// If the tail alone exceeds maxUnits it is truncated with an ellipsis and returned. Otherwise the body is truncated as needed to fit the remaining units
// (reserving one unit between body and tail when both are present). If truncating the body yields an empty string, the tail is returned; otherwise the
// function returns the body, a newline, then the tail.
func formatNotificationCodeBlockWithTailPriority(lines, tail []string, maxUnits int) string {
	tailContent := joinNotificationLines(tail)
	tailUnits := telegramMessageUnits(tailContent)
	if tailUnits >= maxUnits {
		return truncateToTelegramUnitsWithEllipsis(tailContent, maxUnits)
	}

	bodyLimit := maxUnits - tailUnits
	if len(lines) > 0 && len(tail) > 0 {
		bodyLimit--
	}
	bodyContent := truncateToTelegramUnitsWithEllipsis(joinNotificationLines(lines), bodyLimit)
	if bodyContent == "" {
		return tailContent
	}

	return bodyContent + "\n" + tailContent
}

// truncateToTelegramUnitsWithEllipsis truncates message to at most maxUnits Telegram units and appends "..." when truncation occurs.
// If the message already fits within maxUnits, it is returned unchanged. When truncation is needed, the function reserves space
// for the ellipsis and returns the truncated message followed by "...". If maxUnits is too small to include the full ellipsis,
// it returns a truncated ellipsis that fits within maxUnits.
func truncateToTelegramUnitsWithEllipsis(message string, maxUnits int) string {
	truncated := truncateToTelegramUnits(message, maxUnits)
	if truncated == message {
		return truncated
	}

	ellipsisUnits := telegramMessageUnits("...")
	if maxUnits <= ellipsisUnits {
		return truncateToTelegramUnits("...", maxUnits)
	}

	return truncateToTelegramUnits(message, maxUnits-ellipsisUnits) + "..."

}

// telegramMessageUnits reports the length of message measured in Telegram "units".
// It counts each rune with code point > 0xFFFF as 2 units and all other runes as 1 unit.
// This metric is used to evaluate message size against Telegram's unit-based limits.
func telegramMessageUnits(message string) int {
	units := 0
	for _, r := range message {
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
	}
	return units
}

// truncateToTelegramUnits truncates the message to at most maxUnits Telegram units.
//
// A rune with code point > 0xFFFF counts as 2 units; all other runes count as 1 unit.
// Truncation preserves whole runes and stops before adding a rune that would exceed
// maxUnits. If maxUnits is less than or equal to zero, an empty string is returned.
// This function does not append ellipsis or any other marker when truncation occurs.
func truncateToTelegramUnits(message string, maxUnits int) string {
	if maxUnits <= 0 {
		return ""
	}

	var builder strings.Builder
	units := 0
	for _, r := range message {
		runeUnits := 1
		if r > 0xFFFF {
			runeUnits = 2
		}
		if units+runeUnits > maxUnits {
			break
		}
		builder.WriteRune(r)
		units += runeUnits
	}

	return builder.String()
}

func shouldThrottleAIReasoning(reasoning AIReasoningNotification) bool {
	action := strings.ToLower(strings.TrimSpace(reasoning.Action))
	summary := strings.ToLower(strings.TrimSpace(reasoning.Summary))
	category := strings.ToLower(strings.TrimSpace(reasoning.ReasonCategory))
	if category == "" {
		category = strings.ToLower(strings.TrimSpace(reasoning.HoldCategory))
	}

	if action == "hold" && reasoning.ConfidenceKnown && reasoning.Confidence <= 0.45 {
		return true
	}
	if category == "llm_timeout" || category == "llm_parse_contract" || category == "execution_unavailable" {
		return true
	}

	return strings.Contains(summary, "runtime error") ||
		strings.Contains(summary, "skipped due to ai/runtime error") ||
		strings.Contains(summary, "returned no trade decision") ||
		strings.Contains(summary, "paused temporarily after repeated runtime failures")
}

func aiReasoningThrottleKey(chatID int64, reasoning AIReasoningNotification) string {
	if !shouldThrottleAIReasoning(reasoning) {
		return ""
	}

	decisionType := strings.ToLower(strings.TrimSpace(reasoning.DecisionType))
	if decisionType == "" {
		decisionType = "unknown"
	}

	summary := strings.ToLower(strings.TrimSpace(reasoning.Summary))
	reasonText := strings.ToLower(strings.Join(reasoning.Reasons, " "))

	category := "hold_low_conf"
	if strings.Contains(summary, "runtime") ||
		strings.Contains(summary, "error") ||
		strings.Contains(summary, "returned no trade decision") ||
		strings.Contains(reasonText, "execution unavailable") ||
		strings.Contains(reasonText, "model response parse fallback") ||
		strings.Contains(reasonText, "failed to parse ai decision") ||
		strings.Contains(reasonText, "llm completion failed") {
		category = "runtime_hold"
	}

	return fmt.Sprintf("ai_reasoning:%d:%s:%s", chatID, decisionType, category)
}

// generateProgressBar generates a visual progress bar
func (ns *NotificationService) generateProgressBar(percent, width int) string {
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}

	filled := (percent * width) / 100
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return fmt.Sprintf("[%s] %d%%", bar, percent)
}

// NotifySystemAlert sends a system alert notification to a specific chat via Telegram.
// It formats the alert with severity-appropriate emoji and dispatches it through the
// existing Telegram delivery pipeline (gRPC first, HTTP fallback).
func (ns *NotificationService) NotifySystemAlert(ctx context.Context, chatID int64, alert SystemAlert) (err error) {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifySystemAlert", map[string]string{
		"chat_id": fmt.Sprintf("%d", chatID),
		"level":   string(alert.Level),
		"source":  alert.Source,
	})
	defer func() { observability.FinishSpan(span, err) }()

	message := ns.formatSystemAlertMessage(alert)
	message = truncateToTelegramUnitsWithEllipsis(message, ns.telegramMaxMessageUnits)

	if err = ns.sendTelegramMessage(spanCtx, chatID, message); err != nil {
		ns.logger.Error("Failed to send system alert notification",
			"chat_id", chatID,
			"level", alert.Level,
			"source", alert.Source,
			"error", err,
		)
		return
	}

	ns.logger.Info("Sent system alert notification",
		"chat_id", chatID,
		"level", alert.Level,
		"source", alert.Source,
	)

	return
}

// BroadcastSystemAlert sends a system alert to all eligible users (those with Telegram
// chat IDs configured and not blocked). It is used by AlertService to fan-out error and
// critical alerts to operators.
func (ns *NotificationService) BroadcastSystemAlert(ctx context.Context, alert SystemAlert) (err error) {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.BroadcastSystemAlert", map[string]string{
		"level":  string(alert.Level),
		"source": alert.Source,
	})
	defer func() { observability.FinishSpan(span, err) }()

	if ns.db == nil {
		ns.logger.Error("Cannot broadcast system alert: database not available",
			"level", alert.Level,
			"source", alert.Source,
		)
		err = fmt.Errorf("cannot broadcast system alert: database not available")
		return
	}

	users, queryErr := ns.getSystemAlertRecipients(spanCtx)
	if queryErr != nil {
		ns.logger.Error("Failed to get recipients for system alert broadcast",
			"error", queryErr,
		)
		err = fmt.Errorf("failed to get recipients for alert broadcast: %w", queryErr)
		return
	}

	if len(users) == 0 {
		ns.logger.Info("No eligible recipients found for system alert broadcast")
		return
	}

	message := ns.formatSystemAlertMessage(alert)
	message = truncateToTelegramUnitsWithEllipsis(message, ns.telegramMaxMessageUnits)

	var sendErrs []error
	sentCount := 0
	for _, user := range users {
		if user.TelegramChatID == nil {
			continue
		}
		chatID, parseErr := strconv.ParseInt(*user.TelegramChatID, 10, 64)
		if parseErr != nil {
			ns.logger.Error("Invalid chat ID for user during alert broadcast",
				"user_id", user.ID,
				"chat_id", *user.TelegramChatID,
				"error", parseErr,
			)
			sendErrs = append(sendErrs, fmt.Errorf("user %s: invalid chat ID: %w", user.ID, parseErr))
			continue
		}

		if sendErr := ns.sendTelegramMessage(spanCtx, chatID, message); sendErr != nil {
			ns.logger.Error("Failed to send system alert to user",
				"user_id", user.ID,
				"chat_id", chatID,
				"error", sendErr,
			)
			sendErrs = append(sendErrs, fmt.Errorf("user %s: send failed: %w", user.ID, sendErr))
			continue
		}
		sentCount++
	}

	ns.logger.Info("Broadcast system alert completed",
		"level", alert.Level,
		"source", alert.Source,
		"total_users", len(users),
		"sent_count", sentCount,
		"failed_count", len(sendErrs),
	)

	if len(sendErrs) > 0 {
		err = fmt.Errorf("broadcast completed with %d failures: %w",
			len(sendErrs), errors.Join(sendErrs...))
	}
	return
}

// NotifyMonitoringAlert sends an autonomous monitoring alert to a specific chat via Telegram.
// It formats the alert message with a monitoring-specific header.
func (ns *NotificationService) NotifyMonitoringAlert(ctx context.Context, chatID int64, message string) (err error) {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "NotificationService.NotifyMonitoringAlert", map[string]string{
		"chat_id": fmt.Sprintf("%d", chatID),
	})
	defer observability.FinishSpan(span, err)

	formatted := formatMonitoringAlertMessage(message)
	formatted = truncateToTelegramUnitsWithEllipsis(formatted, ns.telegramMaxMessageUnits)

	if err = ns.sendTelegramMessage(spanCtx, chatID, formatted); err != nil {
		ns.logger.Error("Failed to send monitoring alert notification",
			"chat_id", chatID,
			"error", err,
		)
		return
	}

	ns.logger.Info("Sent monitoring alert notification",
		"chat_id", chatID,
	)

	return
}

func formatMonitoringAlertMessage(message string) string {
	return fmt.Sprintf("🚨 AUTONOMOUS MONITORING ALERT\n\n%s\n\nTime: %s", message, time.Now().UTC().Format(time.RFC3339))
}

// getSystemAlertRecipients returns all users with Telegram configured and not blocked.
// Unlike getEligibleUsers (which is arbitrage-specific), this method does not filter
// by user alert preferences — system-level error/critical alerts are always sent.
func (ns *NotificationService) getSystemAlertRecipients(ctx context.Context) ([]userModels.User, error) {
	if ns.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	query := `
		SELECT id, email, telegram_chat_id, subscription_tier, created_at, updated_at
		FROM users
		WHERE telegram_chat_id IS NOT NULL
		  AND telegram_chat_id != ''
		  AND (telegram_blocked IS NULL OR telegram_blocked = false)
	`

	rows, err := ns.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query system alert recipients: %w", err)
	}
	defer rows.Close()

	var users []userModels.User
	for rows.Next() {
		var user userModels.User
		if err := rows.Scan(&user.ID, &user.Email, &user.TelegramChatID, &user.SubscriptionTier, &user.CreatedAt, &user.UpdatedAt); err != nil {
			ns.logger.Error("Failed to scan user row for system alert recipients", "error", err)
			return nil, fmt.Errorf("failed to scan user row: %w", err)
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		ns.logger.Error("Error iterating user rows for system alert recipients", "error", err)
		return nil, fmt.Errorf("failed iterating system alert recipients: %w", err)
	}

	return users, nil
}

// formatSystemAlertMessage formats a SystemAlert into a human-readable Telegram message.
func (ns *NotificationService) formatSystemAlertMessage(alert SystemAlert) string {
	var levelEmoji string
	switch alert.Level {
	case AlertLevelInfo:
		levelEmoji = "ℹ️"
	case AlertLevelWarning:
		levelEmoji = "⚠️"
	case AlertLevelError:
		levelEmoji = "🔴"
	case AlertLevelCritical:
		levelEmoji = "🚨"
	default:
		levelEmoji = "⚠️"
	}

	lines := []string{
		fmt.Sprintf("%s System Alert [%s]", levelEmoji, strings.ToUpper(string(alert.Level))),
		"",
		fmt.Sprintf("Source: %s", alert.Source),
		fmt.Sprintf("Message: %s", alert.Message),
	}

	if len(alert.Details) > 0 {
		lines = append(lines, "", "Details:")
		keys := make([]string, 0, len(alert.Details))
		for key := range alert.Details {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("• %s: %v", key, alert.Details[key]))
		}
	}

	lines = append(lines, "", fmt.Sprintf("Time: %s", alert.Timestamp.UTC().Format(time.RFC3339)))

	return joinNotificationLines(lines)
}

// joinNotificationLines joins lines with newlines
func joinNotificationLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
