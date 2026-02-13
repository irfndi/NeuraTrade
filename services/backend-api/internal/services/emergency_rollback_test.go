package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEmergencyRollbackService_CreateAndGetSnapshot(t *testing.T) {
	svc := NewEmergencyRollbackService(nil)

	err := svc.CreateSnapshot(context.Background(), "scalping", "v1.0", "skill content here", "initial version")
	assert.NoError(t, err)

	snapshots := svc.GetSnapshots("scalping")
	assert.Len(t, snapshots, 1)
	assert.Equal(t, "v1.0", snapshots[0].Version)
	assert.Equal(t, "initial version", snapshots[0].Reason)
}

func TestEmergencyRollbackService_GetLatestSnapshot(t *testing.T) {
	svc := NewEmergencyRollbackService(nil)

	svc.CreateSnapshot(context.Background(), "scalping", "v1.0", "content v1", "initial")
	svc.CreateSnapshot(context.Background(), "scalping", "v1.1", "content v1.1", "updated params")

	latest := svc.GetLatestSnapshot("scalping")
	assert.NotNil(t, latest)
	assert.Equal(t, "v1.1", latest.Version)
}

func TestEmergencyRollbackService_Rollback(t *testing.T) {
	svc := NewEmergencyRollbackService(nil)

	svc.CreateSnapshot(context.Background(), "scalping", "v1.0", "old content", "initial")
	svc.CreateSnapshot(context.Background(), "scalping", "v1.1", "new content", "updated")

	content, err := svc.Rollback(context.Background(), "scalping", "v1.0")
	assert.NoError(t, err)
	assert.Equal(t, "old content", content)
}

func TestEmergencyRollbackService_RollbackToLastKnownGood(t *testing.T) {
	svc := NewEmergencyRollbackService(nil)

	svc.CreateSnapshot(context.Background(), "scalping", "v1.0", "content v1", "initial")
	time.Sleep(10 * time.Millisecond)
	svc.CreateSnapshot(context.Background(), "scalping", "v1.1", "content v1.1", "updated")
	time.Sleep(10 * time.Millisecond)
	beforeBad := time.Now()
	svc.CreateSnapshot(context.Background(), "scalping", "v1.2", "broken content", "bad update")

	content, err := svc.RollbackToLastKnownGood(context.Background(), "scalping", beforeBad)
	assert.NoError(t, err)
	assert.Equal(t, "content v1.1", content)
}

func TestEmergencyRollbackService_GetVersionHistory(t *testing.T) {
	svc := NewEmergencyRollbackService(nil)

	svc.CreateSnapshot(context.Background(), "scalping", "v1.0", "content v1", "initial")
	svc.CreateSnapshot(context.Background(), "scalping", "v1.1", "content v1.1", "updated")

	history := svc.GetVersionHistory("scalping")
	assert.Len(t, history, 2)
}

func TestEmergencyRollbackService_DeleteOldSnapshots(t *testing.T) {
	svc := NewEmergencyRollbackService(nil)

	oldTime := time.Now().Add(-48 * time.Hour)
	svc.snapshots["scalping"] = []SkillSnapshot{
		{SkillID: "scalping", Version: "v1.0", CreatedAt: oldTime},
		{SkillID: "scalping", Version: "v1.1", CreatedAt: time.Now()},
	}

	err := svc.DeleteOldSnapshots(context.Background(), "scalping", 24*time.Hour)
	assert.NoError(t, err)

	snaps := svc.GetSnapshots("scalping")
	assert.Len(t, snaps, 1)
	assert.Equal(t, "v1.1", snaps[0].Version)
}
