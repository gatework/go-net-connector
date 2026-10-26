package parallel

import (
	"context"
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
	if e != context.Canceled || calls != 1 || !r[0].Started || r[1].Started {
		t.Fatalf("%+v %v calls=%d", r, e, calls)
	}
}
