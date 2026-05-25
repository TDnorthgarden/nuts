package task

import (
	"context"
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/db"
)

func TestArchiveCleaner(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	now := time.Now()
	oldArchived := now.Add(-48 * time.Hour)
	recentArchived := now.Add(-1 * time.Hour)

	// Create old archived task
	oldTask := &Task{
		ID:         "old-task",
		State:      TaskStateCompleted,
		CreatedAt:  now.Add(-49 * time.Hour),
		ArchivedAt: &oldArchived,
	}
	store.Create(oldTask)

	// Create recent archived task
	recentTask := &Task{
		ID:         "recent-task",
		State:      TaskStateCompleted,
		CreatedAt:  now.Add(-2 * time.Hour),
		ArchivedAt: &recentArchived,
	}
	store.Create(recentTask)

	// Create running task (should not be cleaned)
	runningTask := &Task{
		ID:        "running-task",
		State:     TaskStateProcessing,
		CreatedAt: now.Add(-49 * time.Hour),
	}
	store.Create(runningTask)

	// Clean up tasks older than 24 hours
	cleaner := NewArchiveCleaner(store, 1, time.Hour)
	cleaner.cleanup()

	// old-task should be deleted
	_, err := store.Get("old-task")
	if err == nil {
		t.Fatal("expected old-task to be deleted")
	}

	// recent-task should remain
	_, err = store.Get("recent-task")
	if err != nil {
		t.Fatal("expected recent-task to remain")
	}

	// running-task should remain (no CompletedAt)
	_, err = store.Get("running-task")
	if err != nil {
		t.Fatal("expected running-task to remain")
	}
}

func TestArchiveCleanerStartStop(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	cleaner := NewArchiveCleaner(store, 30, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cleaner.Start(ctx)

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)

	cancel()
	cleaner.Stop()
}
