package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/event"
)

func testEvent(id, kind string, sequence uint64, payload map[string]any) event.Event {
	b, _ := json.Marshal(payload)
	pid := 123
	return event.Event{SchemaVersion: 1, EventID: id, EventType: kind, OccurredAt: time.Date(2026, 7, 10, 8, 0, int(sequence), 0, time.UTC),
		InstanceID: "local-test", ProducerID: "plugin-test", ProcessID: &pid, Sequence: sequence, Source: "plugin", Payload: b}
}

func TestInsertDeduplicateAndReduceOutOfOrder(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	terminal := testEvent("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "agent.completed", 2, map[string]any{"runId": "run-1", "sessionId": "session-1", "durationMs": 100})
	started := testEvent("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "agent.started", 1, map[string]any{"runId": "run-1", "sessionId": "session-1", "provider": "openai", "model": "gpt-5"})
	result, err := repo.InsertEvents(ctx, []event.Event{terminal, started, terminal})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Inserted) != 2 || result.Duplicates != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	detail, err := repo.RunDetail(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail["status"] != "completed" {
		t.Fatalf("terminal state regressed: %#v", detail)
	}
	if detail["provider"] != "openai" || detail["model"] != "gpt-5" {
		t.Fatalf("late start fields missing: %#v", detail)
	}
}

func TestNewerGatewayStartWinsOverLateStop(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	start := testEvent("cccccccc-cccc-4ccc-8ccc-cccccccccccc", "gateway.started", 2, map[string]any{})
	start.OccurredAt = time.Date(2026, 7, 10, 8, 1, 0, 0, time.UTC)
	stop := testEvent("dddddddd-dddd-4ddd-8ddd-dddddddddddd", "gateway.stopped", 1, map[string]any{})
	stop.OccurredAt = time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	if _, err := repo.InsertEvents(ctx, []event.Event{start, stop}); err != nil {
		t.Fatal(err)
	}
	instances, err := repo.ListInstances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0]["status"] != "up" {
		t.Fatalf("late stop regressed instance: %#v", instances)
	}
}
