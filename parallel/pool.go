// Package parallel runs bounded, ordered batches with cooperative cancellation.
package parallel

import (
	"context"
	"errors"
	"sync"
)

type Result[T any] struct {
	Value   T
	Err     error
	Started bool
}

func Map[T, R any](ctx context.Context, workers int, input []T, run func(context.Context, T) (R, error)) ([]Result[R], error) {
	if workers == 0 {
		workers = 4
	}
	if workers < 1 || workers > 500 || run == nil {
		return nil, errors.New("parallel: invalid pool")
	}
	values := append([]T(nil), input...)
	results := make([]Result[R], len(values))
	next := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(workers, len(values)) {
		wg.Go(func() {
			for {
				mu.Lock()
				if ctx.Err() != nil || next >= len(values) {
					mu.Unlock()
					return
				}
				index := next
				next++
				results[index].Started = true
				mu.Unlock()
				results[index].Value, results[index].Err = run(ctx, values[index])
			}
		})
	}
	wg.Wait()
	return results, ctx.Err()
}
