// Package requestcontext carries immutable request metadata between SDK layers.
package requestcontext

import "context"

// key identifies values stored in request contexts.
type key int

const (
	trackerKey key = iota
	recorderKey
)

// Recorder collects Infrahub node IDs observed during a request workflow.
type Recorder interface {
	// RecordNodeIDs records node identifiers observed during a request workflow.
	RecordNodeIDs(...string)
}

// WithTracker returns a child context carrying a request tracker override.
func WithTracker(ctx context.Context, tracker string) context.Context {
	return context.WithValue(ctx, trackerKey, tracker)
}

// Tracker returns the request tracker override, if present.
func Tracker(ctx context.Context) (string, bool) {
	tracker, ok := ctx.Value(trackerKey).(string)
	return tracker, ok
}

// WithRecorder returns a child context carrying recorder.
func WithRecorder(ctx context.Context, recorder Recorder) context.Context {
	return context.WithValue(ctx, recorderKey, recorder)
}

// RecordNodeIDs records IDs when the context contains a recorder.
func RecordNodeIDs(ctx context.Context, ids ...string) {
	if recorder, ok := ctx.Value(recorderKey).(Recorder); ok && recorder != nil {
		recorder.RecordNodeIDs(ids...)
	}
}
