package eventlog

import (
	"context"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/log"
)

// Cleaner 定时清理器
type Cleaner struct {
	eventLog EventLog
	interval time.Duration
	retain   time.Duration
	logger   log.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewCleaner 创建清理器
func NewCleaner(eventLog EventLog, interval, retain time.Duration) *Cleaner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Cleaner{
		eventLog: eventLog,
		interval: interval,
		retain:   retain,
		logger:   log.GetDefault(),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动清理器
func (c *Cleaner) Start() {
	c.wg.Add(1)
	go c.run()
}

// Stop 停止清理器
func (c *Cleaner) Stop() {
	c.cancel()
	c.wg.Wait()
}

func (c *Cleaner) run() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

func (c *Cleaner) cleanup() {
	before := time.Now().Add(-c.retain)
	deleted, err := c.eventLog.Cleanup(before)
	if err != nil {
		c.logger.Warn("Event log cleanup failed", log.Error(err))
		return
	}
	if deleted > 0 {
		c.logger.Info("Event log cleanup completed",
			log.Int("deleted", deleted))
	}
}
