package trading

import "errors"

// Strategy is an interface that describes desired entry and exit trading behavior
type Strategy interface {
	ShouldEnter(index int, record *TradingRecord) bool
	ShouldExit(index int, record *TradingRecord) bool
}

// ErrNilRule is returned when a RuleStrategy is created with a nil rule
var ErrNilRule = errors.New("rule cannot be nil")

// RuleStrategy is a strategy based on rules and an unstable period. The two rules determine whether a position should
// be created or closed, and unstable period is an index before no positions should be created or exited
type RuleStrategy struct {
	EntryRule      Rule
	ExitRule       Rule
	UnstablePeriod int
}

// NewRuleStrategy creates a new RuleStrategy with validation
// Returns error if EntryRule or ExitRule is nil
func NewRuleStrategy(entryRule, exitRule Rule, unstablePeriod int) (RuleStrategy, error) {
	if entryRule == nil || exitRule == nil {
		return RuleStrategy{}, ErrNilRule
	}
	return RuleStrategy{
		EntryRule:      entryRule,
		ExitRule:       exitRule,
		UnstablePeriod: unstablePeriod,
	}, nil
}

// ShouldEnter will return true when index is greater than unstable period and entry rule is satisfied
func (rs RuleStrategy) ShouldEnter(index int, record *TradingRecord) bool {
	if rs.EntryRule == nil || record == nil {
		return false
	}

	if index > rs.UnstablePeriod && record.CurrentPosition().IsNew() {
		return rs.EntryRule.IsSatisfied(index, record)
	}

	return false
}

// ShouldExit will return true when index is greater than unstable period and exit rule is satisfied
func (rs RuleStrategy) ShouldExit(index int, record *TradingRecord) bool {
	if rs.ExitRule == nil || record == nil {
		return false
	}

	if index > rs.UnstablePeriod && record.CurrentPosition().IsOpen() {
		return rs.ExitRule.IsSatisfied(index, record)
	}

	return false
}
