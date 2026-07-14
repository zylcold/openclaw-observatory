package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/event"
	"github.com/zylcold/openclaw-observatory/internal/storage"
)

func TestParseCPUTime(t *testing.T) {
	for input, want := range map[string]float64{"01:02.50": 62.5, "02:03:04": 7384, "1-00:00:01": 86401} {
		got, err := parseCPUTime(input)
		if err != nil || got != want {
			t.Fatalf("%s: got %v err %v want %v", input, got, err, want)
		}
	}
}

func TestDiskSpace(t *testing.T) {
	total, available, err := diskSpace()
	if err != nil || total <= 0 || available < 0 || available > total {
		t.Fatalf("unexpected disk space total=%d available=%d err=%v", total, available, err)
	}
}

func TestCollectorEmitsCrashAfterConsecutiveSamplingFailures(t *testing.T) {
	repo, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	pid := os.Getpid()
	if _, err := repo.InsertEvents(context.Background(), []event.Event{{
		SchemaVersion: 1, EventID: event.NewID(), EventType: "gateway.started", OccurredAt: time.Now().UTC(),
		InstanceID: "test", ProducerID: "test", ProcessID: &pid, Sequence: 1, Source: "test", Payload: []byte(`{}`),
	}}); err != nil {
		t.Fatal(err)
	}
	var emitted []event.Event
	collector := NewCollector(repo, time.Hour, func(_ context.Context, events []event.Event) error {
		emitted = append(emitted, events...)
		return nil
	})
	collector.sampleFn = func(context.Context, int) (map[string]any, error) { return nil, errors.New("permission denied") }
	for i := 0; i < samplingFailureThreshold; i++ {
		collector.collect(context.Background())
	}
	if len(emitted) != 1 || emitted[0].EventType != "gateway.crashed" {
		t.Fatalf("expected one crash event after repeated sampling failures, got %#v", emitted)
	}
}
