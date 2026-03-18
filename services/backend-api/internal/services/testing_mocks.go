package services

import (
	"context"

	"github.com/stretchr/testify/mock"
)

// MockSignalAggregator implements SignalAggregatorInterface for testing within the services package
type MockSignalAggregator struct {
	mock.Mock
}

func (m *MockSignalAggregator) AggregateArbitrageSignals(ctx context.Context, input ArbitrageSignalInput) ([]*AggregatedSignal, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*AggregatedSignal), args.Error(1)
}

func (m *MockSignalAggregator) AggregateTechnicalSignals(ctx context.Context, input TechnicalSignalInput) ([]*AggregatedSignal, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*AggregatedSignal), args.Error(1)
}

func (m *MockSignalAggregator) DeduplicateSignals(ctx context.Context, signals []*AggregatedSignal) ([]*AggregatedSignal, error) {
	args := m.Called(ctx, signals)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*AggregatedSignal), args.Error(1)
}

func (m *MockSignalAggregator) AggregateMicrostructureSignals(ctx context.Context, input MicrostructureSignalInput) ([]*AggregatedSignal, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*AggregatedSignal), args.Error(1)
}
