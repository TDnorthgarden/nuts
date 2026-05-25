package task

import (
	"context"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

// ArchiveCleaner periodically removes archived tasks older than the retention period.
type ArchiveCleaner struct {
	store     TaskStore
	retention time.Duration
	interval  time.Duration
	ticker    *time.Ticker
	stopCh    chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
	logger    log.Logger
	metrics   common.MetricsRecorder
}

// NewArchiveCleaner creates a new archive cleaner.
func NewArchiveCleaner(store TaskStore, retentionDays int, interval time.Duration) *ArchiveCleaner {
	retention := time.Duration(retentionDays) * 24 * time.Hour
	return &ArchiveCleaner{
		store:     store,
		retention: retention,
		interval:  interval,
		stopCh:    make(chan struct{}),
		logger:    log.GetDefault(),
	}
}

// Start begins the periodic cleanup goroutine.
func (c *ArchiveCleaner) Start(ctx context.Context) {
	c.ticker = time.NewTicker(c.interval)
	c.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.logger.Error("panic in archive cleaner", log.Any("recover", r))
			}
		}()
		defer c.wg.Done()
		defer c.ticker.Stop()
		// Run initial cleanup
		c.cleanup()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case <-c.ticker.C:
				c.cleanup()
			}
		}
	}()
}

// SetMetrics 设置度量收集器
func (c *ArchiveCleaner) SetMetrics(m common.MetricsRecorder) {
	c.metrics = m
}

// Stop signals the cleaner to stop and waits for it to finish.
func (c *ArchiveCleaner) Stop() {
	c.closeOnce.Do(func() {
		close(c.stopCh)
	})
	c.wg.Wait()
}

func (c *ArchiveCleaner) cleanup() {
	cutoff := time.Now().Add(-c.retention)

	tasks, err := c.store.List(TaskFilter{IncludeArchived: true})
	if err != nil {
		c.logger.Error("archive cleanup: list failed", log.Error(err))
		return
	}

	var deleted int
	for _, task := range tasks {
		if task.ArchivedAt != nil && task.ArchivedAt.Before(cutoff) {
			if err := c.store.Delete(task.ID); err != nil {
				c.logger.Error("archive cleanup: delete failed", log.String("task_id", task.ID), log.Error(err))
				continue
			}
			deleted++
		}
	}

	if deleted > 0 {
		c.logger.Info("archive cleanup: removed old tasks",
			log.Int("count", deleted),
			log.String("retention", c.retention.String()))
		if c.metrics != nil {
			for i := 0; i < deleted; i++ {
				c.metrics.TaskDeleted()
			}
		}
	}
}
