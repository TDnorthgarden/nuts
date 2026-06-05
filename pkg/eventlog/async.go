package eventlog

import (
	"context"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/log"
)

// asyncEventLog 异步写入包装器
// 通过 buffered channel 将写入操作异步化，不阻塞主流程
type asyncEventLog struct {
	inner  EventLog
	ch     chan *EventLogEntry
	logger log.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewAsyncEventLog 创建异步 EventLog 包装器
func NewAsyncEventLog(inner EventLog, bufferSize int) EventLog {
	if bufferSize <= 0 {
		bufferSize = 4096
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &asyncEventLog{
		inner:  inner,
		ch:     make(chan *EventLogEntry, bufferSize),
		logger: log.GetDefault(),
		ctx:    ctx,
		cancel: cancel,
	}
	a.wg.Add(1)
	go a.processLoop()
	return a
}

func (a *asyncEventLog) Append(_ context.Context, entry *EventLogEntry) error {
	if entry == nil {
		return nil
	}
	select {
	case a.ch <- entry:
		return nil
	default:
		// channel 满，丢弃不阻塞主流程
		return nil
	}
}

func (a *asyncEventLog) QueryByTraceID(traceID string) (*TraceTimeline, error) {
	return a.inner.QueryByTraceID(traceID)
}

func (a *asyncEventLog) QueryByTaskID(taskID string) ([]EventLogEntry, error) {
	return a.inner.QueryByTaskID(taskID)
}

func (a *asyncEventLog) Query(filter LogFilter) ([]EventLogEntry, error) {
	return a.inner.Query(filter)
}

func (a *asyncEventLog) Cleanup(before time.Time) (int, error) {
	return a.inner.Cleanup(before)
}

func (a *asyncEventLog) Close() error {
	a.cancel()
	close(a.ch)
	a.wg.Wait()
	return a.inner.Close()
}

func (a *asyncEventLog) processLoop() {
	defer a.wg.Done()
	for entry := range a.ch {
		if err := a.inner.Append(a.ctx, entry); err != nil {
			a.logger.Warn("Failed to append event log entry",
				log.String("entry_id", entry.ID),
				log.Error(err))
		}
	}
}
