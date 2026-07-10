package storage

import (
	"context"
	"database/sql"
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

func TestModelStatsIncludesCacheTokensAndSortsByTotalUsage(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	events := []event.Event{
		testEvent("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "llm.completed", 1, map[string]any{
			"callId": "call-high", "provider": "bailian", "model": "qwen", "inputTokens": 10,
			"outputTokens": 5, "cacheReadTokens": 100,
		}),
		testEvent("ffffffff-ffff-4fff-8fff-ffffffffffff", "llm.completed", 2, map[string]any{
			"callId": "call-low", "provider": "zai", "model": "glm", "inputTokens": 20,
			"outputTokens": 10,
		}),
	}
	if _, err := repo.InsertEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	stats, err := repo.ModelStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("unexpected model stats: %#v", stats)
	}
	if stats[0]["model"] != "qwen" || stats[0]["cacheReadTokens"] != float64(100) {
		t.Fatalf("cache tokens missing or ordering incorrect: %#v", stats)
	}
}

func TestRunUsageUpdateWithoutCallIDMergesIntoLatestCall(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	events := []event.Event{
		testEvent("11111111-1111-4111-8111-111111111111", "llm.started", 1, map[string]any{
			"callId": "run-usage:model:1", "runId": "run-usage", "provider": "bailian", "model": "qwen3.7-plus",
		}),
		testEvent("22222222-2222-4222-8222-222222222222", "llm.completed", 2, map[string]any{
			"callId": "run-usage:model:1", "runId": "run-usage", "provider": "bailian", "model": "qwen3.7-plus",
		}),
		testEvent("33333333-3333-4333-8333-333333333333", "llm.completed", 3, map[string]any{
			"runId": "run-usage", "provider": "bailian", "model": "qwen3.7-plus", "accountingUpdate": true,
			"inputTokens": 1549, "outputTokens": 73, "cacheReadTokens": 48896,
		}),
	}
	if _, err := repo.InsertEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	stats, err := repo.ModelStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0]["requests"] != int64(1) {
		t.Fatalf("accounting update created an extra request: %#v", stats)
	}
	if stats[0]["inputTokens"] != float64(1549) || stats[0]["outputTokens"] != float64(73) || stats[0]["cacheReadTokens"] != float64(48896) {
		t.Fatalf("accounting update did not merge into the call: %#v", stats)
	}
}

func TestOpenMigratesV1AndListsRunsByAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES(1,?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO agent_runs(instance_id,run_id,status,started_at) VALUES('local-test','run-historical','completed','2026-07-10T07:59:00Z')`); err != nil {
		t.Fatal(err)
	}
	historicalPayload := `{"runId":"run-historical","agentId":"historical-agent"}`
	if _, err := legacy.Exec(`INSERT INTO events(event_id,schema_version,event_type,occurred_at,instance_id,producer_id,sequence,source,payload_json,received_at)
		VALUES('66666666-6666-4666-8666-666666666666',1,'agent.completed','2026-07-10T08:00:00Z','local-test','plugin-test',1,'plugin',?,'2026-07-10T08:00:00Z')`, historicalPayload); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	repo, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	events := []event.Event{
		testEvent("11111111-1111-4111-8111-111111111111", "session.started", 1, map[string]any{
			"sessionId": "session-tom", "agentId": "tom",
		}),
		testEvent("22222222-2222-4222-8222-222222222222", "agent.started", 2, map[string]any{
			"runId": "run-tom", "sessionId": "session-tom", "agentId": "tom",
		}),
		testEvent("33333333-3333-4333-8333-333333333333", "agent.started", 3, map[string]any{
			"runId": "run-session-fallback", "sessionId": "session-tom",
		}),
		testEvent("44444444-4444-4444-8444-444444444444", "subagent.started", 4, map[string]any{
			"subagentId": "run-dick", "agentId": "dick",
		}),
		testEvent("55555555-5555-4555-8555-555555555555", "agent.started", 5, map[string]any{
			"runId": "run-dick",
		}),
	}
	if _, err := repo.InsertEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	runs, err := repo.ListRuns(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	agents := make(map[string]string, len(runs))
	for _, run := range runs {
		agents[run["runId"].(string)] = run["agentId"].(string)
	}
	if agents["run-tom"] != "tom" || agents["run-session-fallback"] != "tom" || agents["run-dick"] != "dick" || agents["run-historical"] != "historical-agent" {
		t.Fatalf("run agent attribution missing: %#v", runs)
	}
	var migrated int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, CurrentSchemaVersion).Scan(&migrated); err != nil || migrated != 1 {
		t.Fatalf("latest migration not recorded: count=%d err=%v", migrated, err)
	}
}
