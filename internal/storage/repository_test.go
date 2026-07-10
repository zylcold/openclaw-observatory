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
	stats, err := repo.ModelStats(ctx, ListOptions{})
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
	stats, err := repo.ModelStats(ctx, ListOptions{})
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

func TestV3AnalyticsAndSessionTimeline(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	events := []event.Event{
		testEvent("70000000-0000-4000-8000-000000000001", "session.started", 1, map[string]any{"sessionId": "session-v3", "agentId": "tom"}),
		testEvent("70000000-0000-4000-8000-000000000002", "agent.started", 2, map[string]any{"runId": "run-v3", "sessionId": "session-v3", "agentId": "tom", "provider": "bailian", "model": "qwen"}),
		testEvent("70000000-0000-4000-8000-000000000003", "llm.started", 3, map[string]any{"callId": "llm-v3", "runId": "run-v3", "sessionId": "session-v3", "provider": "bailian", "model": "qwen"}),
		testEvent("70000000-0000-4000-8000-000000000004", "llm.completed", 4, map[string]any{"callId": "llm-v3", "runId": "run-v3", "sessionId": "session-v3", "durationMs": 400, "inputTokens": 20, "outputTokens": 5, "costUsd": 0.01}),
		testEvent("70000000-0000-4000-8000-000000000005", "tool.started", 5, map[string]any{"toolCallId": "tool-v3", "runId": "run-v3", "sessionId": "session-v3", "toolName": "exec"}),
		testEvent("70000000-0000-4000-8000-000000000006", "tool.failed", 6, map[string]any{"toolCallId": "tool-v3", "runId": "run-v3", "sessionId": "session-v3", "durationMs": 100, "errorCategory": "timeout"}),
		testEvent("70000000-0000-4000-8000-000000000007", "mcp.started", 7, map[string]any{"toolCallId": "mcp-v3", "runId": "run-v3", "sessionId": "session-v3", "toolName": "search", "toolOwner": "web"}),
		testEvent("70000000-0000-4000-8000-000000000008", "mcp.completed", 8, map[string]any{"toolCallId": "mcp-v3", "runId": "run-v3", "sessionId": "session-v3", "durationMs": 150}),
		testEvent("70000000-0000-4000-8000-000000000009", "subagent.started", 9, map[string]any{"subagentId": "sub-v3", "parentRunId": "run-v3", "agentId": "worker", "provider": "bailian", "model": "qwen"}),
		testEvent("70000000-0000-4000-8000-000000000010", "subagent.completed", 10, map[string]any{"subagentId": "sub-v3", "parentRunId": "run-v3", "outcome": "done"}),
		testEvent("70000000-0000-4000-8000-000000000011", "agent.failed", 11, map[string]any{"runId": "run-v3", "sessionId": "session-v3", "agentId": "tom", "durationMs": 900, "errorCategory": "model_error"}),
		testEvent("70000000-0000-4000-8000-000000000012", "session.failed", 12, map[string]any{"sessionId": "session-v3", "agentId": "tom", "reason": "model_error"}),
		testEvent("70000000-0000-4000-8000-000000000013", "resource.sampled", 13, map[string]any{"cpuSecondsTotal": 10, "residentMemoryBytes": 1024, "diskTotalBytes": 1000, "diskAvailableBytes": 250}),
	}
	if _, err := repo.InsertEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	opts := ListOptions{From: "2026-07-10T07:59:00Z", To: "2026-07-10T08:01:00Z"}

	agents, err := repo.AgentStats(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0]["agentId"] != "tom" || agents[0]["runs"] != int64(1) || agents[0]["totalTokens"] != float64(25) || agents[0]["toolCalls"] != int64(2) {
		t.Fatalf("unexpected agent stats: %#v", agents)
	}
	sessions, err := repo.ListSessions(ctx, ListOptions{AgentID: "tom"})
	if err != nil || len(sessions) != 1 {
		t.Fatalf("agent filter did not drive sessions: %#v err=%v", sessions, err)
	}
	runs, err := repo.ListRuns(ctx, ListOptions{AgentID: "worker"})
	if err != nil || len(runs) != 0 {
		t.Fatalf("agent filter did not drive runs: %#v err=%v", runs, err)
	}

	detail, err := repo.SessionDetail(ctx, "session-v3")
	if err != nil {
		t.Fatal(err)
	}
	timeline := detail["timeline"].([]map[string]any)
	kinds := map[string]bool{}
	for _, item := range timeline {
		kinds[item["kind"].(string)] = true
	}
	for _, kind := range []string{"llm", "tool", "mcp", "subagent"} {
		if !kinds[kind] {
			t.Fatalf("timeline missing %s: %#v", kind, timeline)
		}
	}

	series, err := repo.TimeSeries(ctx, opts, 60)
	if err != nil {
		t.Fatal(err)
	}
	points := series["points"].([]map[string]any)
	models := series["models"].([]map[string]any)
	heatmap := series["agents"].([]map[string]any)
	if len(points) != 1 || points[0]["llmRequests"] != int64(1) || points[0]["toolCalls"] != int64(2) || points[0]["diskUsedPercent"] != float64(75) || len(models) != 1 || len(heatmap) != 1 {
		t.Fatalf("unexpected timeseries: %#v", series)
	}

	mcp, err := repo.ListMCPCalls(ctx, opts)
	if err != nil || len(mcp) != 1 || mcp[0]["agentId"] != "tom" {
		t.Fatalf("unexpected mcp calls: %#v err=%v", mcp, err)
	}
	subagents, err := repo.ListSubagentRuns(ctx, opts)
	if err != nil || len(subagents) != 1 || subagents[0]["agentId"] != "worker" {
		t.Fatalf("unexpected subagent runs: %#v err=%v", subagents, err)
	}
	errors, err := repo.ErrorStats(ctx, opts)
	if err != nil || len(errors) != 2 {
		t.Fatalf("unexpected error stats: %#v err=%v", errors, err)
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
