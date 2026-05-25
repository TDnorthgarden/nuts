package datasource

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

type DropPolicy int

const (
	DropOldest DropPolicy = iota
	DropNewest
	Block
)

type BufferedDataSource struct {
	source     DataSource
	bufferSize int
	dropPolicy DropPolicy

	buffer *RingBuffer
	stats  BufferStats
	mu     sync.RWMutex

	outCh     chan<- *common.Event
	readyCh   chan struct{}
	readyOnce sync.Once
	logger    log.Logger

	ownCtx  context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup
	started atomic.Bool
}

type BufferStats struct {
	BufferedEvents  int64
	DroppedEvents   int64
	ProcessedEvents int64
	FullCount       int64
}

type RingBuffer struct {
	data []*common.Event
	head int
	tail int
	size int
	cap  int
	mu   sync.Mutex
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		data: make([]*common.Event, capacity),
		cap:  capacity,
	}
}

func (rb *RingBuffer) Push(event *common.Event) (dropped *common.Event) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.size == rb.cap {
		switch {
		case rb.cap == 0:
			return event
		default:
			dropped = rb.data[rb.head]
			rb.head = (rb.head + 1) % rb.cap
			rb.size--
		}
	}

	rb.data[rb.tail] = event
	rb.tail = (rb.tail + 1) % rb.cap
	rb.size++
	return nil
}

func (rb *RingBuffer) Pop() *common.Event {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.size == 0 {
		return nil
	}

	event := rb.data[rb.head]
	rb.head = (rb.head + 1) % rb.cap
	rb.size--
	return event
}

func (rb *RingBuffer) Size() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.size
}

func (rb *RingBuffer) IsFull() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.size == rb.cap
}

func NewBufferedDataSource(source DataSource, bufferSize int, dropPolicy DropPolicy) *BufferedDataSource {
	return &BufferedDataSource{
		source:     source,
		bufferSize: bufferSize,
		dropPolicy: dropPolicy,
		buffer:     NewRingBuffer(bufferSize),
		logger:     log.GetDefault(),
		readyCh:    make(chan struct{}),
	}
}

func (b *BufferedDataSource) SetLogger(logger log.Logger) {
	b.logger = logger
}

func (b *BufferedDataSource) ParseConfig(config map[string]interface{}) error {
	return b.source.ParseConfig(config)
}

func (b *BufferedDataSource) Start(ctx context.Context, eventCh chan<- *common.Event) error {
	if !b.started.CompareAndSwap(false, true) {
		return nil
	}

	b.ownCtx, b.stop = context.WithCancel(ctx)
	b.outCh = eventCh

	internalCh := make(chan *common.Event, 1000)

	if err := b.source.Start(ctx, internalCh); err != nil {
		b.stop()
		b.started.Store(false)
		return err
	}

	b.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("bufferEvents panic", log.Any("recover", r))
			}
		}()
		defer b.wg.Done()
		b.bufferEvents(internalCh)
	}()

	b.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("flushEvents panic", log.Any("recover", r))
			}
		}()
		defer b.wg.Done()
		b.flushEvents()
	}()

	b.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("ready wait panic", log.Any("recover", r))
			}
		}()
		defer b.wg.Done()
		select {
		case <-b.source.Ready():
			b.readyOnce.Do(func() { close(b.readyCh) })
		case <-b.ownCtx.Done():
		}
	}()

	return nil
}

func (b *BufferedDataSource) Stop() error {
	if !b.started.CompareAndSwap(true, false) {
		return nil
	}

	b.stop()

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		b.logger.Warn("Buffered data source stop timeout, forcing exit")
	}

	return b.source.Stop()
}

func (b *BufferedDataSource) Health() error {
	return b.source.Health()
}

func (b *BufferedDataSource) GetStats() *DataSourceStats {
	return b.source.GetStats()
}

func (b *BufferedDataSource) Ready() <-chan struct{} {
	return b.readyCh
}

func (b *BufferedDataSource) GetBufferStats() BufferStats {
	return BufferStats{
		BufferedEvents:  atomic.LoadInt64(&b.stats.BufferedEvents),
		DroppedEvents:   atomic.LoadInt64(&b.stats.DroppedEvents),
		ProcessedEvents: atomic.LoadInt64(&b.stats.ProcessedEvents),
		FullCount:       atomic.LoadInt64(&b.stats.FullCount),
	}
}

func (b *BufferedDataSource) bufferEvents(inCh <-chan *common.Event) {
	for {
		select {
		case <-b.ownCtx.Done():
			return
		case event := <-inCh:
			b.mu.Lock()
			currentSize := b.buffer.Size()
			b.mu.Unlock()

			switch b.dropPolicy {
			case DropOldest:
				dropped := b.buffer.Push(event)
				if dropped != nil {
					atomic.AddInt64(&b.stats.DroppedEvents, 1)
				}
				atomic.AddInt64(&b.stats.BufferedEvents, 1)

			case DropNewest:
				b.mu.Lock()
				if b.buffer.IsFull() {
					atomic.AddInt64(&b.stats.DroppedEvents, 1)
					b.mu.Unlock()
					continue
				}
				b.mu.Unlock()
				b.buffer.Push(event)
				atomic.AddInt64(&b.stats.BufferedEvents, 1)

			case Block:
				for {
					b.mu.Lock()
					if !b.buffer.IsFull() {
						b.buffer.Push(event)
						atomic.AddInt64(&b.stats.BufferedEvents, 1)
						b.mu.Unlock()
						break
					}
					b.mu.Unlock()

					select {
					case <-b.ownCtx.Done():
						return
					case <-time.After(10 * time.Millisecond):
					}
				}
			}

			if currentSize == b.bufferSize {
				atomic.AddInt64(&b.stats.FullCount, 1)
			}
		}
	}
}

func (b *BufferedDataSource) flushEvents() {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-b.ownCtx.Done():
			return
		case <-ticker.C:
			for {
				event := b.buffer.Pop()
				if event == nil {
					break
				}

				select {
				case b.outCh <- event:
					atomic.AddInt64(&b.stats.ProcessedEvents, 1)
				case <-b.ownCtx.Done():
					return
				}
			}
		}
	}
}
