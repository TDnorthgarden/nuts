package component

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

// WorkerPool 工作池
// 管理并发处理事件的工作协程
type WorkerPool struct {
	maxWorkers int
	eventChan  <-chan *common.Event
	handler    EventHandler
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc

	// 统计
	processed int64
	failed    int64
}

// NewWorkerPool 创建工作池
func NewWorkerPool(maxWorkers int) *WorkerPool {
	return &WorkerPool{
		maxWorkers: maxWorkers,
	}
}

// Start 启动工作池
func (p *WorkerPool) Start(eventChan <-chan *common.Event, handler EventHandler) {
	p.eventChan = eventChan
	p.handler = handler
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// 启动 worker
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// Stop 停止工作池
func (p *WorkerPool) Stop() {
	p.cancel()
	p.wg.Wait()
}

// worker 工作协程
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case event := <-p.eventChan:
			if event == nil {
				continue
			}

			// 处理事件
			if err := p.handler(event); err != nil {
				atomic.AddInt64(&p.failed, 1)
			} else {
				atomic.AddInt64(&p.processed, 1)
			}

		case <-p.ctx.Done():
			return
		}
	}
}

// Stats 获取统计信息
func (p *WorkerPool) Stats() (processed int64, failed int64) {
	return atomic.LoadInt64(&p.processed), atomic.LoadInt64(&p.failed)
}
