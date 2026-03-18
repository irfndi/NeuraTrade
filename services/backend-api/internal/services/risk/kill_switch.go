package risk

import (
	"context"
	"sync"
)

type KillSwitch struct {
	mu      sync.RWMutex
	engaged bool
	reason  string
}

func NewKillSwitch() *KillSwitch {
	return &KillSwitch{}
}

func (k *KillSwitch) Engage(_ context.Context, reason string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.engaged = true
	k.reason = reason
	return nil
}

func (k *KillSwitch) Disengage(_ context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.engaged = false
	k.reason = ""
	return nil
}

func (k *KillSwitch) IsEngaged() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.engaged
}
