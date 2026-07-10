package event

import (
	"encoding/json"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	pid := 42
	e := Event{
		SchemaVersion: 1, EventID: NewID(), EventType: "agent.started",
		OccurredAt: time.Now().UTC(), InstanceID: "local-test", ProducerID: "plugin-test",
		ProcessID: &pid, Sequence: 1, Source: "plugin", Payload: json.RawMessage(`{"runId":"run-1"}`),
	}
	if err := e.Validate(time.Now()); err != nil {
		t.Fatal(err)
	}
	e.EventType = "made.up"
	if err := e.Validate(time.Now()); err == nil {
		t.Fatal("expected invalid event type")
	}
}

func TestNewID(t *testing.T) {
	if got := NewID(); len(got) != 36 {
		t.Fatalf("unexpected UUID length: %q", got)
	}
}
