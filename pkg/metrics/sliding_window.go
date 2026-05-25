package metrics

import (
	"sync"
	"time"
)

// slidingWindow 滑动窗口计数器
// 用于计算一段时间内的事件速率
type slidingWindow struct {
	mu       sync.Mutex
	buckets  []bucket
	size     time.Duration
	interval time.Duration
	total    int64
}

type bucket struct {
	start time.Time
	count int64
}

// newSlidingWindow 创建滑动窗口
// size: 窗口总时长
// interval: 桶间隔（默认 size/60）
func newSlidingWindow(size time.Duration, interval time.Duration) *slidingWindow {
	if interval <= 0 {
		interval = size / 60
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
	}
	return &slidingWindow{
		buckets:  make([]bucket, 0, 60),
		size:     size,
		interval: interval,
	}
}

// Add 向窗口添加计数
func (w *slidingWindow) Add(n int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	w.evict(now)

	currentBucket := w.currentBucket(now)
	currentBucket.count += n
	w.total += n
}

// Rate 返回窗口内的事件速率（每秒）
func (w *slidingWindow) Rate() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.evict(time.Now())
	if w.size == 0 {
		return 0
	}
	return float64(w.total) / w.size.Seconds()
}

// Total 返回窗口内的事件总数
func (w *slidingWindow) Total() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.evict(time.Now())
	return w.total
}

// evict 清理过期的桶
func (w *slidingWindow) evict(now time.Time) {
	cutoff := now.Add(-w.size)
	newTotal := int64(0)
	validStart := 0

	for i, b := range w.buckets {
		if b.start.After(cutoff) {
			newTotal += b.count
			validStart = i
			break
		}
	}

	if validStart > 0 {
		w.buckets = w.buckets[validStart:]
	}
	w.total = newTotal
}

// currentBucket 获取当前时间对应的桶
func (w *slidingWindow) currentBucket(now time.Time) *bucket {
	if len(w.buckets) == 0 {
		w.buckets = append(w.buckets, bucket{start: now})
		return &w.buckets[0]
	}

	last := &w.buckets[len(w.buckets)-1]
	if now.Sub(last.start) < w.interval {
		return last
	}

	w.buckets = append(w.buckets, bucket{start: now})
	return &w.buckets[len(w.buckets)-1]
}
