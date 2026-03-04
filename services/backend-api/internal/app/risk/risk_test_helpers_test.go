package risk

import (
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
)

func waitForActorRunning(t *testing.T, ref *actor.Ref, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for !ref.IsRunning() {
		if time.Now().After(deadline) {
			t.Fatalf("actor %s did not start within %s", ref.ID(), timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
