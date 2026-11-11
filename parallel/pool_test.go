package parallel

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolBoundsAndOrders(t *testing.T) {
	var active, peak atomic.Int32
	values := make([]int, 30)
	for i := range values {
		values[i] = i
	}
	results, e := Map(t.Context(), 3, values, func(ctx context.Context, v int) (int, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		return v * 2, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if peak.Load() > 3 {
		t.Fatal(peak.Load())
	}
	for i, r := range results {
		if !r.Started || r.Value != i*2 {
			t.Fatal(i, r)
		}
	}
}
func TestCanceledPoolDoesNotDispatchMore(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	r, e := Map(ctx, 1, []int{1, 2, 3}, func(context.Context, int) (int, error) { calls++; cancel(); return 1, nil })
	if !errors.Is(e, context.Canceled) || calls != 1 || !r[0].Started || r[1].Started {
		t.Fatalf("%+v %v calls=%d", r, e, calls)
	}
}

func TestMapGoexitCancelsBatchAndMarksStartedTask(t *testing.T) {
	results, err := Map(t.Context(), 1, []int{1, 2, 3}, func(context.Context, int) (int, error) { runtime.Goexit(); return 0, nil })
	if err == nil || !results[0].Started || results[0].Err == nil || results[1].Started {
		t.Fatalf("lost worker exit: %#v %v", results, err)
	}
}

func TestMapGoexitStopsOtherWorkers(t *testing.T) {
	entered := make(chan struct{})
	results, err := Map(t.Context(), 2, []int{0, 1, 2}, func(ctx context.Context, n int) (int, error) {
		if n == 0 {
			<-entered
			runtime.Goexit()
		}
		close(entered)
		<-ctx.Done()
		return n, ctx.Err()
	})
	if !errors.Is(err, ErrWorkerExit) || !errors.Is(results[0].Err, ErrWorkerExit) || !errors.Is(results[1].Err, context.Canceled) || results[2].Started {
		t.Fatalf("worker cancellation: %v %v", results, err)
	}
}

func TestMapGoexitPreservesParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results, err := Map(ctx, 1, []int{1, 2}, func(context.Context, int) (int, error) {
		cancel()
		runtime.Goexit()
		return 0, nil
	})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrWorkerExit) {
		t.Fatalf("parent cancellation masked worker exit: %v", err)
	}
	if !errors.Is(results[0].Err, ErrWorkerExit) || results[1].Started {
		t.Fatalf("worker evidence: %+v", results)
	}
}
