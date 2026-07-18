package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/event"
)

func TestInsertEventsSplitsLargeBatchesAndAggregatesResults(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	events := make([]event.Event, 0, insertBatchSize+1)
	for i := 0; i < insertBatchSize; i++ {
		events = append(events, testEvent(fmt.Sprintf("10000000-0000-4000-8000-%012d", i), "gateway.heartbeat", uint64(i), map[string]any{}))
	}
	// This duplicate lands in the second transaction and verifies that results
	// are accumulated across transaction boundaries.
	events = append(events, events[0])
	result, err := repo.InsertEvents(context.Background(), events)
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != insertBatchSize+1 || len(result.Inserted) != insertBatchSize || result.Duplicates != 1 {
		t.Fatalf("unexpected split batch result: %+v", result)
	}
	count, err := repo.Count(context.Background(), "events")
	if err != nil || count != int64(insertBatchSize) {
		t.Fatalf("unexpected stored event count: %d err=%v", count, err)
	}
}

func TestRecentAnomaliesSupportsEmptyDatabase(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	rows, err := repo.RecentAnomalies(context.Background(), ListOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no anomalies, got %#v", rows)
	}
}

func TestTraceTelemetryRetryAndTerminalStateProjection(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	events := []event.Event{
		testEvent("61000000-0000-4000-8000-000000000001", "session.started", 1, map[string]any{"sessionId": "session-trace", "agentId": "main"}),
		testEvent("61000000-0000-4000-8000-000000000002", "agent.started", 2, map[string]any{
			"runId": "run-trace", "sessionId": "session-trace", "traceId": "trace-1", "spanId": "run-span",
		}),
		testEvent("61000000-0000-4000-8000-000000000003", "llm.completed", 3, map[string]any{
			"callId": "llm-span", "runId": "run-trace", "sessionId": "session-trace", "traceId": "trace-1",
			"spanId": "llm-span", "parentSpanId": "run-span", "provider": "openai", "model": "gpt",
			"durationMs": 900, "timeToFirstByteMs": 100, "timeToFirstTokenMs": 140, "generationDurationMs": 700,
			"stopReason": "stop", "attempt": 2, "retryReason": "rate_limit", "requestPayloadBytes": 1200,
			"responseStreamBytes": 2400, "inputTokens": 100, "outputTokens": 20, "costUsd": 0.04,
		}),
		testEvent("61000000-0000-4000-8000-000000000004", "llm.retried", 4, map[string]any{
			"runId": "run-trace", "sessionId": "session-trace", "traceId": "trace-1", "spanId": "retry-span",
			"parentSpanId": "run-span", "attempt": 2, "fromModel": "gpt-a", "toModel": "gpt-b", "reason": "rate_limit",
		}),
		testEvent("61000000-0000-4000-8000-000000000005", "tool.completed", 5, map[string]any{
			"toolCallId": "tool-span", "runId": "run-trace", "sessionId": "session-trace", "traceId": "trace-1",
			"spanId": "tool-span", "parentSpanId": "run-span", "toolName": "exec", "durationMs": 50, "attempt": 2,
		}),
		testEvent("61000000-0000-4000-8000-000000000006", "tool.started", 6, map[string]any{
			"toolCallId": "tool-span", "runId": "run-trace", "sessionId": "session-trace", "toolName": "exec",
		}),
	}
	if _, err := repo.InsertEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.SessionDetail(ctx, "session-trace")
	if err != nil {
		t.Fatal(err)
	}
	timeline := detail["timeline"].([]map[string]any)
	if len(timeline) != 3 {
		t.Fatalf("expected llm, retry and tool spans, got %#v", timeline)
	}
	var llm, tool map[string]any
	for _, item := range timeline {
		switch item["kind"] {
		case "llm":
			llm = item
		case "tool":
			tool = item
		}
	}
	if llm["traceId"] != "trace-1" || llm["parentSpanId"] != "run-span" || llm["timeToFirstTokenMs"] != float64(140) {
		t.Fatalf("missing trace telemetry: %#v", llm)
	}
	if tool["status"] != "completed" {
		t.Fatalf("late tool start regressed terminal status: %#v", tool)
	}
	summary := detail["summary"].(map[string]any)
	if summary["retries"] != int64(1) || summary["totalTokens"] != float64(120) {
		t.Fatalf("unexpected trace summary: %#v", summary)
	}
}

func TestDeleteBeforePurgesInBoundedBatches(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	ctx := context.Background()
	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO events(event_id,schema_version,event_type,occurred_at,instance_id,producer_id,sequence,source,payload_json,received_at) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	for i := 0; i < retentionDeleteBatchSize+1; i++ {
		if _, err := stmt.ExecContext(ctx, fmt.Sprintf("20000000-0000-4000-8000-%012d", i), 1, "gateway.heartbeat", old, "test", "test", i, "test", "{}", old); err != nil {
			stmt.Close()
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	deleted, err := repo.deleteBefore(ctx, "events", "occurred_at", time.Now().UTC().Add(-24*time.Hour))
	if err != nil || deleted != retentionDeleteBatchSize+1 {
		t.Fatalf("unexpected batched purge: deleted=%d err=%v", deleted, err)
	}
	count, err := repo.Count(ctx, "events")
	if err != nil || count != 0 {
		t.Fatalf("expected all expired rows purged: count=%d err=%v", count, err)
	}
}

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
	toolSeries := series["tools"].([]map[string]any)
	if len(points) != 1 || points[0]["llmRequests"] != int64(1) || points[0]["toolCalls"] != int64(2) || points[0]["diskUsedPercent"] != float64(75) || len(models) != 1 || len(heatmap) != 1 || len(toolSeries) != 2 {
		t.Fatalf("unexpected timeseries: %#v", series)
	}
	var execSeries map[string]any
	for _, tool := range toolSeries {
		if tool["tool"] == "exec" {
			execSeries = tool
		}
	}
	if execSeries == nil || execSeries["p95DurationMs"] != float64(100) || execSeries["p99DurationMs"] != float64(100) {
		t.Fatalf("tool timeseries tail latency missing: %#v", toolSeries)
	}
	tools, err := repo.ToolStats(ctx, opts)
	if err != nil || len(tools) != 2 {
		t.Fatalf("unexpected tool stats: %#v err=%v", tools, err)
	}
	var execStats map[string]any
	for _, tool := range tools {
		if tool["tool"] == "exec" {
			execStats = tool
		}
	}
	if execStats == nil || execStats["p95DurationMs"] != float64(100) || execStats["p99DurationMs"] != float64(100) || execStats["timeouts"] != int64(1) || execStats["failureRate"] != float64(100) {
		t.Fatalf("tool tail latency or timeout aggregation missing: %#v", tools)
	}
	failedTools, err := repo.ToolStats(ctx, ListOptions{From: opts.From, To: opts.To, Status: "failed"})
	if err != nil || len(failedTools) != 1 || failedTools[0]["tool"] != "exec" {
		t.Fatalf("tool status filter did not reach aggregate query: %#v err=%v", failedTools, err)
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
