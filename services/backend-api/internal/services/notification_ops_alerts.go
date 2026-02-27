package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/observability"
)

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
		fmt.Sprintf("%s **Quest Progress Update**", statusEmoji),
		"",
		fmt.Sprintf("**%s**", progress.QuestName),
		fmt.Sprintf("Progress: %d/%d (%d%%)", progress.Current, progress.Target, progress.Percent),
	}

	if progress.Status == "completed" {
		lines = append(lines, "", "🎉 Quest completed!")
	} else if progress.TimeRemaining != "" {
		lines = append(lines, fmt.Sprintf("Time remaining: %s", progress.TimeRemaining))
	}

	progressBar := ns.generateProgressBar(progress.Percent, 10)
	lines = append(lines, "", progressBar)

	return fmt.Sprintf("```\n%s\n```", joinNotificationLines(lines))
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
		fmt.Sprintf("%s **Risk Event Alert**", severityEmoji),
		"",
		fmt.Sprintf("**Type:** %s", event.EventType),
		fmt.Sprintf("**Severity:** %s", event.Severity),
		"",
		event.Message,
	}

	if len(event.Details) > 0 {
		lines = append(lines, "", "**Details:**")
		for key, value := range event.Details {
			lines = append(lines, fmt.Sprintf("• %s: %s", key, value))
		}
	}

	lines = append(lines, "", fmt.Sprintf("_Time: %s_", time.Now().UTC().Format(time.RFC3339)))

	return fmt.Sprintf("```\n%s\n```", joinNotificationLines(lines))
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
		"💰 **Fund Milestone Reached!**",
		"",
		fmt.Sprintf("**%s**", milestone.Achievement),
		"",
		fmt.Sprintf("Current: %s", milestone.CurrentValue),
		fmt.Sprintf("Target: %s", milestone.TargetValue),
		fmt.Sprintf("Progress: %d%%", milestone.PercentReached),
	}

	progressBar := ns.generateProgressBar(milestone.PercentReached, 20)
	lines = append(lines, "", progressBar)

	return fmt.Sprintf("```\n%s\n```", joinNotificationLines(lines))
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

// formatAIReasoningMessage formats an AI reasoning notification message
func (ns *NotificationService) formatAIReasoningMessage(reasoning AIReasoningNotification) string {
	confidenceKnown := reasoning.ConfidenceKnown
	if !confidenceKnown &&
		strings.TrimSpace(reasoning.ReasonCategory) == "" &&
		strings.TrimSpace(reasoning.HoldCategory) == "" {
		// Backward-compatible fallback for legacy callers.
		confidenceKnown = true
	}
	category := strings.TrimSpace(reasoning.ReasonCategory)
	if category == "" {
		category = strings.TrimSpace(reasoning.HoldCategory)
	}

	lines := []string{
		"🤖 **AI Trading Decision**",
		"",
		fmt.Sprintf("**Type:** %s", reasoning.DecisionType),
	}

	if confidenceKnown {
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
		lines = append(lines, fmt.Sprintf("**Confidence:** %s %d%%", confidenceEmoji, confidencePercent))
	} else {
		lines = append(lines, "**Confidence:** ⚪ N/A (runtime-degraded)")
	}

	lines = append(lines,
		"",
		fmt.Sprintf("**Summary:** %s", reasoning.Summary),
	)
	if category != "" {
		lines = append(lines, fmt.Sprintf("**Reason Category:** %s", category))
	}
	if strings.TrimSpace(reasoning.UnblockCondition) != "" {
		lines = append(lines, fmt.Sprintf("**Unblock Condition:** %s", strings.TrimSpace(reasoning.UnblockCondition)))
	}
	if strings.TrimSpace(reasoning.AttemptWindowProgress) != "" {
		lines = append(lines, fmt.Sprintf("**Attempt Window:** %s", strings.TrimSpace(reasoning.AttemptWindowProgress)))
	}

	if len(reasoning.Reasons) > 0 {
		lines = append(lines, "", "**Key Factors:**")
		for i, reason := range reasoning.Reasons {
			if i < 5 {
				lines = append(lines, fmt.Sprintf("• %s", reason))
			}
		}
		if len(reasoning.Reasons) > 5 {
			lines = append(lines, fmt.Sprintf("• ... and %d more factors", len(reasoning.Reasons)-5))
		}
	}

	if reasoning.Action != "" {
		lines = append(lines, "", fmt.Sprintf("**Recommended Action:** %s", reasoning.Action))
	}

	return fmt.Sprintf("```\n%s\n```", joinNotificationLines(lines))
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
