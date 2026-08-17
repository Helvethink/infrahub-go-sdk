package requestcontext

import (
	"context"
	"reflect"
	"testing"
)

type testRecorder struct{ ids []string }

func (r *testRecorder) RecordNodeIDs(ids ...string) { r.ids = append(r.ids, ids...) }

func TestTracker(t *testing.T) {
	t.Parallel()
	if tracker, ok := Tracker(context.Background()); ok || tracker != "" {
		t.Fatalf("Tracker(background) = %q, %t", tracker, ok)
	}
	ctx := WithTracker(context.Background(), "workflow")
	if tracker, ok := Tracker(ctx); !ok || tracker != "workflow" {
		t.Fatalf("Tracker(ctx) = %q, %t", tracker, ok)
	}
}

func TestRecordNodeIDs(t *testing.T) {
	t.Parallel()
	recorder := &testRecorder{}
	RecordNodeIDs(context.Background(), "ignored")
	RecordNodeIDs(WithRecorder(context.Background(), recorder), "one", "two")
	if !reflect.DeepEqual(recorder.ids, []string{"one", "two"}) {
		t.Fatalf("ids = %#v", recorder.ids)
	}
}
