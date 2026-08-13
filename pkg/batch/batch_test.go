package batch_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/batch"
)

func TestMapBoundsConcurrencyAndPreservesInputOrder(t *testing.T) {
	t.Parallel()
	var active, peak atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	done := make(chan struct{})
	var results []batch.Result[int]
	var runErr error
	go func() {
		defer close(done)
		results, runErr = batch.Map(context.Background(), []int{3, 1, 2}, func(_ context.Context, input int) (int, error) {
			current := active.Add(1)
			for previous := peak.Load(); current > previous; previous = peak.Load() {
				if peak.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			active.Add(-1)
			return input * 10, nil
		}, batch.Options{Concurrency: 2})
	}()
	<-started
	<-started
	if got := peak.Load(); got != 2 {
		t.Fatalf("peak concurrency = %d", got)
	}
	close(release)
	<-done
	if runErr != nil {
		t.Fatal(runErr)
	}
	want := []batch.Result[int]{{Index: 0, Value: 30}, {Index: 1, Value: 10}, {Index: 2, Value: 20}}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("Map() = %#v, want %#v", results, want)
	}
}

func TestRunFailFastCancelsStartedJobs(t *testing.T) {
	t.Parallel()
	failure := errors.New("failed")
	waitingStarted := make(chan struct{})
	jobs := []batch.Job[string]{
		func(context.Context) (string, error) {
			<-waitingStarted
			return "", failure
		},
		func(ctx context.Context) (string, error) {
			close(waitingStarted)
			<-ctx.Done()
			return "", ctx.Err()
		},
		func(context.Context) (string, error) { return "should-not-run", nil },
	}
	results, err := batch.Run(context.Background(), jobs, batch.Options{Concurrency: 2})
	var batchError *batch.Error
	if !errors.As(err, &batchError) || batchError.Index != 0 || !errors.Is(err, failure) {
		t.Fatalf("error = %T %v", err, err)
	}
	if len(results) != 2 || results[0].Index != 0 || results[1].Index != 1 || !errors.Is(results[1].Err, context.Canceled) {
		t.Fatalf("results = %#v", results)
	}
}

func TestRunContinueOnErrorStartsEveryJob(t *testing.T) {
	t.Parallel()
	failure := errors.New("failed")
	jobs := []batch.Job[int]{
		func(context.Context) (int, error) { return 1, nil },
		func(context.Context) (int, error) { return 0, failure },
		func(context.Context) (int, error) { return 3, nil },
	}
	results, err := batch.Run(context.Background(), jobs, batch.Options{Concurrency: 1, ContinueOnError: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || !errors.Is(results[1].Err, failure) || results[2].Value != 3 {
		t.Fatalf("results = %#v", results)
	}
}

func TestRunHonorsParentCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	jobs := []batch.Job[int]{func(ctx context.Context) (int, error) {
		cancel()
		<-ctx.Done()
		return 0, ctx.Err()
	}}
	results, err := batch.Run(ctx, jobs, batch.Options{})
	if !errors.Is(err, context.Canceled) || len(results) != 1 {
		t.Fatalf("Run() = %#v, %v", results, err)
	}
}

func TestValidationAndEmptyBatch(t *testing.T) {
	t.Parallel()
	if _, err := batch.Run[int](context.Background(), nil, batch.Options{Concurrency: -1}); err == nil {
		t.Fatal("negative concurrency error = nil")
	}
	results, err := batch.Run[int](context.Background(), nil, batch.Options{})
	if err != nil || results == nil || len(results) != 0 {
		t.Fatalf("empty Run() = %#v, %v", results, err)
	}
	if _, err := batch.Map[int, int](context.Background(), nil, nil, batch.Options{}); err == nil {
		t.Fatal("nil worker error = nil")
	}
	if _, err := batch.Run(context.Background(), []batch.Job[int]{nil}, batch.Options{}); err == nil {
		t.Fatal("nil job error = nil")
	}
}
