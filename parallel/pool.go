// Package parallel runs bounded, ordered batches with cooperative cancellation.
package parallel

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrWorkerExit reports a callback that terminated its worker without returning.
var ErrWorkerExit = errors.New("parallel: worker exited without returning")

// Result is one input's outcome; Started is false for inputs never run.
type Result[T any] struct {
	Value   T
	Err     error
	Started bool
}

// Map runs run over input with at most workers goroutines and returns the
// results in input order. Cancellation stops unstarted inputs.
func Map[T, R any](ctx context.Context, workers int, input []T, run func(context.Context, T) (R, error)) ([]Result[R], error) {
	if workers == 0 {
		workers = 4
	}
	if workers < 1 || workers > 500 || run == nil {
		return nil, errors.New("parallel: invalid pool")
	}
	workerCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	values := append([]T(nil), input...)
	results := make([]Result[R], len(values))
	next := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	var exited atomic.Bool
	for range min(workers, len(values)) {
		wg.Go(func() {
			for {
				mu.Lock()
				if workerCtx.Err() != nil || next >= len(values) {
					mu.Unlock()
					return
				}
				index := next
				next++
				results[index].Started = true
				mu.Unlock()
				func() {
					returned := false
					defer func() {
						if !returned {
							exited.Store(true)
							results[index].Err = ErrWorkerExit
							cancel(ErrWorkerExit)
						}
					}()
					results[index].Value, results[index].Err = run(workerCtx, values[index])
					returned = true
				}()
			}
		})
	}
	wg.Wait()
	if exited.Load() {
		return results, errors.Join(ctx.Err(), ErrWorkerExit)
	}
	return results, ctx.Err()
}
