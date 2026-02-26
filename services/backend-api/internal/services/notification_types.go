package services

import "time"

// ArbitrageOpportunity represents an arbitrage opportunity for notification.
// Note: This struct might be duplicative of models.ArbitrageOpportunity, but used here for JSON marshaling.
type ArbitrageOpportunity struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// BuyExchange is the buying exchange.
	BuyExchange string `json:"buy_exchange"`
	// SellExchange is the selling exchange.
	SellExchange string `json:"sell_exchange"`
	// BuyPrice is the buy price.
	BuyPrice float64 `json:"buy_price"`
	// SellPrice is the sell price.
	SellPrice float64 `json:"sell_price"`
	// ProfitPercent is the profit percentage.
	ProfitPercent float64 `json:"profit_percent"`
	// ProfitAmount is the profit amount.
	ProfitAmount float64 `json:"profit_amount"`
	// Volume is the volume.
	Volume float64 `json:"volume"`
	// Timestamp is the detection time.
	Timestamp time.Time `json:"timestamp"`
	// OpportunityType is the type (arbitrage, technical, etc).
	OpportunityType string `json:"opportunity_type"` // "arbitrage", "technical", "ai_generated"
}

// TechnicalSignalNotification represents a technical analysis signal for notifications.
type TechnicalSignalNotification struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// SignalType is the type of signal.
	SignalType string `json:"signal_type"`
	// Action is the recommended action (buy/sell).
	Action string `json:"action"`
	// SignalText is the description.
	SignalText string `json:"signal_text"`
	// CurrentPrice is the asset price.
	CurrentPrice float64 `json:"current_price"`
	// EntryRange is the recommended entry price range.
	EntryRange string `json:"entry_range"`
	// Targets are the profit targets.
	Targets []Target `json:"targets"`
	// StopLoss is the stop loss level.
	StopLoss StopLoss `json:"stop_loss"`
	// RiskReward is the R:R ratio.
	RiskReward string `json:"risk_reward"`
	// Exchanges is the list of applicable exchanges.
	Exchanges []string `json:"exchanges"`
	// Timeframe is the analysis timeframe.
	Timeframe string `json:"timeframe"`
	// Confidence is the signal confidence level.
	Confidence float64 `json:"confidence"`
	// Timestamp is the signal generation time.
	Timestamp time.Time `json:"timestamp"`
}

// Target represents a profit target price.
type Target struct {
	// Price is the target price.
	Price float64 `json:"price"`
	// Profit is the projected profit percentage.
	Profit float64 `json:"profit"`
}

// StopLoss represents a stop loss level.
type StopLoss struct {
	// Price is the stop loss price.
	Price float64 `json:"price"`
	// Risk is the projected loss percentage.
	Risk float64 `json:"risk"`
}

// TelegramErrorCode represents structured error codes from Telegram service
type TelegramErrorCode string

const (
	TelegramErrorUserBlocked    TelegramErrorCode = "USER_BLOCKED"
	TelegramErrorChatNotFound   TelegramErrorCode = "CHAT_NOT_FOUND"
	TelegramErrorRateLimited    TelegramErrorCode = "RATE_LIMITED"
	TelegramErrorInvalidRequest TelegramErrorCode = "INVALID_REQUEST"
	TelegramErrorNetworkError   TelegramErrorCode = "NETWORK_ERROR"
	TelegramErrorTimeout        TelegramErrorCode = "TIMEOUT"
	TelegramErrorInternal       TelegramErrorCode = "INTERNAL_ERROR"
	TelegramErrorUnknown        TelegramErrorCode = "UNKNOWN"
)

// TelegramSendResult represents the result of sending a Telegram message
type TelegramSendResult struct {
	OK         bool
	MessageID  string
	Error      string
	ErrorCode  TelegramErrorCode
	RetryAfter int32
}

// QuestProgressNotification represents a quest progress notification
type QuestProgressNotification struct {
	QuestID       string
	QuestName     string
	Current       int
	Target        int
	Percent       int
	Status        string
	TimeRemaining string
}

// RiskEventNotification represents a risk event notification
type RiskEventNotification struct {
	EventType string
	Severity  string
	Message   string
	Details   map[string]string
}

// FundMilestoneNotification represents a fund milestone notification
type FundMilestoneNotification struct {
	MilestoneType  string
	CurrentValue   string
	TargetValue    string
	PercentReached int
	Achievement    string
}

// AIReasoningNotification represents an AI reasoning notification
type AIReasoningNotification struct {
	DecisionType    string
	Summary         string
	Confidence      float64
	ConfidenceKnown bool
	ReasonCategory  string
	Reasons         []string
	Action          string
}
