package services

import (
	"context"
	"reflect"

	"github.com/irfndi/neuratrade/internal/database"
)

type DBPool = database.DBPool

type SignalAggregatorInterface interface {
	AggregateArbitrageSignals(ctx context.Context, input ArbitrageSignalInput) ([]*AggregatedSignal, error)
	AggregateTechnicalSignals(ctx context.Context, input TechnicalSignalInput) ([]*AggregatedSignal, error)
	AggregateMicrostructureSignals(ctx context.Context, input MicrostructureSignalInput) ([]*AggregatedSignal, error)
	DeduplicateSignals(ctx context.Context, signals []*AggregatedSignal) ([]*AggregatedSignal, error)
}

func isNilDBPool(db DBPool) bool {
	if db == nil {
		return true
	}

	v := reflect.ValueOf(db)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return true
	}

	if checker, ok := db.(interface{ IsReady() bool }); ok {
		return !checker.IsReady()
	}

	return false
}
