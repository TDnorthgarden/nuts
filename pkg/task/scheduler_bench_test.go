package task

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/db"
)

func BenchmarkTaskStore_Get(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	task := &Task{ID: "test", Name: "test"}
	store.Create(task)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Get("test")
	}
}

func BenchmarkTaskStore_Create(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task := &Task{
			ID:   fmt.Sprintf("task-%d", i),
			Name: "benchmark-task",
		}
		store.Create(task)
	}
}

func BenchmarkTaskStore_Update(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	task := &Task{ID: "test", Name: "test"}
	store.Create(task)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Update(task)
	}
}

func BenchmarkTaskStore_UpdateState(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	task := &Task{ID: "test", Name: "test"}
	store.Create(task)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.UpdateState("test", TaskStateProcessing)
	}
}

// BenchmarkCreateAndTransition_1000PerSec 模拟 4.2.1: 1000 task/s 创建+状态转换
func BenchmarkCreateAndTransition_1000PerSec(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-%d", i)
		task := &Task{ID: id, Name: "bench"}
		if err := store.Create(task); err != nil {
			b.Fatal(err)
		}
		if err := store.UpdateState(id, TaskStateProcessing); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCreateAndTransition_Parallel 4.2.1 并发版：多 goroutine 并发创建+转换
func BenchmarkCreateAndTransition_Parallel(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := fmt.Sprintf("bench-%d", counter.Add(1))
			task := &Task{ID: id, Name: "bench"}
			if err := store.Create(task); err != nil {
				b.Fatal(err)
			}
			if err := store.UpdateState(id, TaskStateProcessing); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkTransitionState_Atomic 4.2.1: 原子化 TransitionState 性能
func BenchmarkTransitionState_Atomic(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	// 预创建任务
	ids := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("task-%d", i)
		ids[i] = id
		task := &Task{ID: id, Name: "bench"}
		if err := store.Create(task); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := store.TransitionState(ids[i], TaskStateProcessing, "bench", "test", nil, false, nil, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkList_ByState_100K 4.2.3: 10 万条任务按状态查询
func BenchmarkList_ByState_100K(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	// 预填充 10 万条任务，分布在不同状态
	states := []TaskState{TaskStatePending, TaskStateProcessing, TaskStateCompleted, TaskStateFailed}
	for i := 0; i < 100000; i++ {
		task := &Task{
			ID:    fmt.Sprintf("task-%d", i),
			Name:  "bench",
			State: states[i%len(states)],
		}
		if err := store.Create(task); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks, err := store.List(TaskFilter{State: TaskStateProcessing})
		if err != nil {
			b.Fatal(err)
		}
		_ = tasks
	}
}

// BenchmarkList_All_100K 10 万条任务全量查询（无状态过滤）
func BenchmarkList_All_100K(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	for i := 0; i < 100000; i++ {
		task := &Task{
			ID:    fmt.Sprintf("task-%d", i),
			Name:  "bench",
			State: TaskStatePending,
		}
		if err := store.Create(task); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tasks, err := store.List(TaskFilter{})
		if err != nil {
			b.Fatal(err)
		}
		_ = tasks
	}
}

// BenchmarkCreateAndTransition_P99 测量创建+转换的延迟分布
func BenchmarkCreateAndTransition_P99(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	latencies := make([]time.Duration, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-%d", i)
		task := &Task{ID: id, Name: "bench"}

		start := time.Now()
		if err := store.Create(task); err != nil {
			b.Fatal(err)
		}
		if err := store.UpdateState(id, TaskStateProcessing); err != nil {
			b.Fatal(err)
		}
		latencies[i] = time.Since(start)
	}
	b.StopTimer()

	// 计算 P50/P99
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)*50/100]
	p99 := latencies[len(latencies)*99/100]
	b.Logf("P50: %v, P99: %v (total ops: %d)", p50, p99, b.N)
}

// BenchmarkList_ByState_P99 测量 List(state) 的延迟分布（10 万条数据，无 Limit）
func BenchmarkList_ByState_P99(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	for i := 0; i < 100000; i++ {
		task := &Task{
			ID:    fmt.Sprintf("task-%d", i),
			Name:  "bench",
			State: TaskStatePending,
		}
		if err := store.Create(task); err != nil {
			b.Fatal(err)
		}
	}

	latencies := make([]time.Duration, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		tasks, err := store.List(TaskFilter{State: TaskStatePending})
		if err != nil {
			b.Fatal(err)
		}
		latencies[i] = time.Since(start)
		_ = tasks
	}
	b.StopTimer()

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)*50/100]
	p99 := latencies[len(latencies)*99/100]
	b.Logf("P50: %v, P99: %v (total ops: %d)", p50, p99, b.N)
}

// BenchmarkList_ByState_Limit20 测量 List(state, limit=20) 的延迟（10 万条数据）
func BenchmarkList_ByState_Limit20(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	for i := 0; i < 100000; i++ {
		task := &Task{
			ID:    fmt.Sprintf("task-%d", i),
			Name:  "bench",
			State: TaskStatePending,
		}
		if err := store.Create(task); err != nil {
			b.Fatal(err)
		}
	}

	latencies := make([]time.Duration, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		tasks, err := store.List(TaskFilter{State: TaskStatePending, Limit: 20})
		if err != nil {
			b.Fatal(err)
		}
		latencies[i] = time.Since(start)
		_ = tasks
	}
	b.StopTimer()

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)*50/100]
	p99 := latencies[len(latencies)*99/100]
	b.Logf("P50: %v, P99: %v (total ops: %d)", p50, p99, b.N)
}

// BenchmarkConcurrentDifferentTasks 不同任务 ID 的并发写竞争
func BenchmarkConcurrentDifferentTasks(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := fmt.Sprintf("task-%d", counter.Add(1))
			task := &Task{ID: id, Name: "bench"}
			if err := store.Create(task); err != nil {
				b.Fatal(err)
			}
			if err := store.UpdateState(id, TaskStateProcessing); err != nil {
				b.Fatal(err)
			}
			if err := store.UpdateState(id, TaskStateCompleted); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkConcurrentSameTask 同一任务 ID 多 goroutine 竞争
func BenchmarkConcurrentSameTask(b *testing.B) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	task := &Task{ID: "shared", Name: "bench"}
	if err := store.Create(task); err != nil {
		b.Fatal(err)
	}

	var wg sync.WaitGroup
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.UpdateState("shared", TaskStateProcessing)
		}()
	}
	wg.Wait()
}
