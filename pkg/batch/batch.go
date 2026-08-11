// Package batch provides bounded, cancellation-aware concurrent execution.
package batch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

const defaultConcurrency = 5

// Job is one context-aware unit of batch work.
type Job[T any] func(context.Context) (T, error)

// Options configures batch execution.
type Options struct {
	// Concurrency bounds simultaneously running jobs. Zero uses five workers.
	Concurrency int
	// ContinueOnError runs remaining jobs and stores each error in its Result.
	ContinueOnError bool
}

// Result is the outcome of one started job. Index identifies its position in
// the input slice. Results returned by Run are sorted by Index.
type Result[T any] struct {
	Index int
	Value T
	Err   error
}

// Error reports the first failed job selected by input order in fail-fast mode.
type Error struct {
	Index int
	Err   error
}

func (e *Error) Error() string {
	return fmt.Sprintf("infrahub: batch job %d failed: %v", e.Index, e.Err)
}

// Unwrap returns the job failure.
func (e *Error) Unwrap() error { return e.Err }

// Run executes jobs with bounded concurrency. In fail-fast mode, the first
// failure cancels the internal context and Run returns *Error after all started
// jobs exit. Jobs must honor context cancellation. In ContinueOnError mode all
// jobs are started and individual failures are available through Result.Err.
func Run[T any](ctx context.Context, jobs []Job[T], options Options) ([]Result[T], error) {
	if options.Concurrency < 0 {
		return nil, fmt.Errorf("infrahub: batch concurrency must not be negative")
	}
	if len(jobs) == 0 {
		return []Result[T]{}, nil
	}
	for index, job := range jobs {
		if job == nil {
			return nil, fmt.Errorf("infrahub: batch job %d must not be nil", index)
		}
	}
	concurrency := options.Concurrency
	if concurrency == 0 {
		concurrency = defaultConcurrency
	}
	if concurrency > len(jobs) {
		concurrency = len(jobs)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tasks := make(chan indexedJob[T])
	completed := make(chan Result[T], len(jobs))
	var workers sync.WaitGroup
	var stopped atomic.Bool
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range tasks {
				if stopped.Load() || runCtx.Err() != nil {
					return
				}
				value, err := item.job(runCtx)
				completed <- Result[T]{Index: item.index, Value: value, Err: err}
				if err != nil && !options.ContinueOnError {
					stopped.Store(true)
					cancel()
					return
				}
			}
		}()
	}
	go func() {
		defer close(tasks)
		for index, job := range jobs {
			select {
			case tasks <- indexedJob[T]{index: index, job: job}:
			case <-runCtx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(completed)
	}()

	results := make([]Result[T], 0, len(jobs))
	for result := range completed {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Index < results[j].Index })
	if err := ctx.Err(); err != nil {
		return results, err
	}
	if !options.ContinueOnError {
		for _, result := range results {
			if result.Err != nil && !errors.Is(result.Err, context.Canceled) {
				return results, &Error{Index: result.Index, Err: result.Err}
			}
		}
		for _, result := range results {
			if result.Err != nil {
				return results, &Error{Index: result.Index, Err: result.Err}
			}
		}
	}
	return results, nil
}

type indexedJob[T any] struct {
	index int
	job   Job[T]
}

// Map applies worker to every input with bounded concurrency.
func Map[In, Out any](ctx context.Context, inputs []In, worker func(context.Context, In) (Out, error), options Options) ([]Result[Out], error) {
	if worker == nil {
		return nil, fmt.Errorf("infrahub: batch worker must not be nil")
	}
	jobs := make([]Job[Out], 0, len(inputs))
	for _, input := range inputs {
		jobs = append(jobs, func(ctx context.Context) (Out, error) { return worker(ctx, input) })
	}
	return Run(ctx, jobs, options)
}
