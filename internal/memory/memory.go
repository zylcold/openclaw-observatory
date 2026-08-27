// Package memory collects OpenClaw agent memory observability data for the
// Observatory daemon:
//
//   - A: active-memory runtime signals from each agent's memory host event log
//     (memory/.dreams/events.jsonl): recall calls, hits/misses, promotions,
//     dreams and active-vs-background recall turns.
//   - B: LM Studio / embedding provider health via periodic HTTP probes.
//   - C: per-agent memory index health snapshots (sources, chunks, dirty,
//     SQLite size, freelist ratio, index freshness) collected at a fixed
//     interval and exposed as gauges.
//
// The memory host events are intentionally not part of the OpenClaw diagnostic
// event stream, so the daemon reads the append-only JSONL audit log directly.
package memory

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/storage"
	_ "modernc.org/sqlite"
)

// Agent identifies one OpenClaw agent with its memory index locations.
type Agent struct {
	Name         string
	WorkspaceDir string
	DBPath       string
}

// Config controls memory collection. Zero values fall back to safe defaults.
type Config struct {
	OpenClawHome  string
	AgentsDir     string
	Overrides     []Agent
	OpenCLawBin   string
	CurlBin       string
	DataDir       string
	EventInterval time.Duration
	IndexInterval time.Duration
	ProbeInterval time.Duration
	ProbeURL      string
	ProbeTimeout  time.Duration
	CLITimeout    time.Duration
}

type recallStats struct {
	Recalls          uint64
	Hits             uint64
	Misses           uint64
	Skipped          uint64
	ActiveTurns      uint64
	BackgroundTurns  uint64
	Promotions       uint64
	PromotionApplied uint64
	DreamsFailed     uint64
	DreamLines       uint64
	DreamPhases      map[string]uint64
}

type agentOffset struct {
	Path string `json:"path"`
	Ino  uint64 `json:"ino"`
	Size int64  `json:"size"`
}

// persistedState is the JSON file storing event-log offsets and cumulative
// recall counters so they survive daemon restarts.
type persistedState struct {
	Offsets  map[string]agentOffset   `json:"offsets"`
	Counters map[string]*recallStats `json:"counters"`
}

// IndexSnapshot is the latest health sample for one agent's memory index.
type IndexSnapshot struct {
	Agent         string
	SampledAt     time.Time
	State         int // 2 = CLI status, 1 = SQLite fallback, 0 = unavailable
	Files         int64
	Chunks        int64
	Dirty         *bool
	Revision      int64
	DBSizeBytes   int64
	WALBytes      int64
	PageCount     int64
	FreeListPages int64
	LastIndexedAt time.Time
	HasIndex      bool
}

// EmbeddingProbeSnapshot mirrors the Gateway probe snapshot for the embedding
// provider (LM Studio) HTTP endpoint.
type EmbeddingProbeSnapshot struct {
	Up                  bool
	DurationSeconds     float64
	ConsecutiveFailures uint64
	ModelCount          int64
	LastProbedAt        *time.Time
	LastSuccessAt       *time.Time
}

// MetricGroup is a ready-to-emit Prometheus metric.
type MetricGroup struct {
	Name string
	Type string
	Help string
	Rows []storage.MetricRow
}

// AgentMemorySnapshot is the JSON-friendly per-agent view served by the
// lightweight /api/v1/memory endpoint. Recall counters are cumulative (they
// persist across daemon restarts); index fields come from the latest 60s
// collection cycle.
type AgentMemorySnapshot struct {
	Name             string            `json:"name"`
	Recalls          uint64            `json:"recalls"`
	Hits             uint64            `json:"hits"`
	Misses           uint64            `json:"misses"`
	Skipped          uint64            `json:"skipped"`
	ActiveTurns      uint64            `json:"activeTurns"`
	BackgroundTurns  uint64            `json:"backgroundTurns"`
	Promotions       uint64            `json:"promotions"`
	PromotionEntries uint64            `json:"promotionEntries"`
	DreamLines       uint64            `json:"dreamLines"`
	DreamsFailed     uint64            `json:"dreamsFailed"`
	DreamPhases      map[string]uint64 `json:"dreamPhases"`
	IndexFiles       int64             `json:"indexFiles"`
	IndexChunks      int64             `json:"indexChunks"`
	IndexDirty       bool              `json:"indexDirty"`
	IndexDirtyKnown  bool              `json:"indexDirtyKnown"`
	IndexRevision    int64             `json:"indexRevision"`
	IndexDBBytes     int64             `json:"indexDBBytes"`
	IndexWALBytes    int64             `json:"indexWALBytes"`
	IndexFreelist    float64           `json:"indexFreelistRatio"`
	IndexFreshness   float64           `json:"indexFreshnessSeconds"`
	IndexLastIndexed float64           `json:"indexLastIndexedAtUnix"`
	IndexState       int               `json:"indexState"`
	IndexSampledAt   float64           `json:"indexSampledAtUnix"`
}

// ProviderMemorySnapshot is the JSON-friendly embedding provider probe view.
type ProviderMemorySnapshot struct {
	Up                   bool    `json:"up"`
	ModelCount           int64   `json:"models"`
	ProbeDurationSeconds float64 `json:"probeDurationSeconds"`
	ConsecutiveFailures  uint64  `json:"consecutiveFailures"`
	LastSuccessAtUnix    float64 `json:"lastSuccessAtUnix"`
}

// MemorySnapshot is the complete in-memory view for /api/v1/memory. Building
// it never touches SQLite; everything is read under the manager lock.
type MemorySnapshot struct {
	GeneratedAt time.Time              `json:"generatedAt"`
	Agents      []AgentMemorySnapshot  `json:"agents"`
	Provider    ProviderMemorySnapshot `json:"provider"`
}

type cliAgentStatus struct {
	AgentID string `json:"agentId"`
	Status  struct {
		Files        int64  `json:"files"`
		Chunks       int64  `json:"chunks"`
		Dirty        bool   `json:"dirty"`
		WorkspaceDir string `json:"workspaceDir"`
		DBPath       string `json:"dbPath"`
	} `json:"status"`
}

type Manager struct {
	cfg     Config
	log     *slog.Logger
	client  *http.Client
	mu      sync.RWMutex
	agents  []Agent
	recalls map[string]*recallStats
	offsets map[string]agentOffset
	index   map[string]IndexSnapshot
	probe   EmbeddingProbeSnapshot
}

func New(cfg Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	if cfg.OpenClawHome == "" {
		cfg.OpenClawHome = filepath.Join(home, ".openclaw")
	}
	if cfg.AgentsDir == "" {
		cfg.AgentsDir = filepath.Join(cfg.OpenClawHome, "agents")
	}
	if cfg.OpenCLawBin == "" {
		cfg.OpenCLawBin = "/opt/homebrew/bin/openclaw"
	}
	if cfg.CurlBin == "" {
		cfg.CurlBin = "/usr/bin/curl"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(home, ".openclaw-observatory")
	}
	if cfg.EventInterval <= 0 {
		cfg.EventInterval = 15 * time.Second
	}
	if cfg.IndexInterval <= 0 {
		cfg.IndexInterval = 60 * time.Second
	}
	if cfg.ProbeInterval <= 0 {
		cfg.ProbeInterval = 30 * time.Second
	}
	if cfg.ProbeURL == "" {
		cfg.ProbeURL = "http://192.168.64.1:1234/v1/models"
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = 3 * time.Second
	}
	if cfg.CLITimeout <= 0 {
		cfg.CLITimeout = 90 * time.Second
	}
	m := &Manager{
		cfg:     cfg,
		log:     logger,
		client:  &http.Client{Timeout: cfg.ProbeTimeout},
		agents:  append([]Agent(nil), cfg.Overrides...),
		recalls: map[string]*recallStats{},
		offsets: map[string]agentOffset{},
		index:   map[string]IndexSnapshot{},
	}
	m.loadState()
	return m
}

// Run drives the event-log scan, index health collection, and provider probe
// loops until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	m.collectIndex()
	m.scanEventLogs()
	m.probeProvider()
	eventTicker := time.NewTicker(m.cfg.EventInterval)
	indexTicker := time.NewTicker(m.cfg.IndexInterval)
	probeTicker := time.NewTicker(m.cfg.ProbeInterval)
	defer eventTicker.Stop()
	defer indexTicker.Stop()
	defer probeTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-eventTicker.C:
			m.scanEventLogs()
		case <-indexTicker.C:
			m.collectIndex()
		case <-probeTicker.C:
			m.probeProvider()
		}
	}
}

func (m *Manager) agentsLocked() []Agent {
	out := make([]Agent, len(m.agents))
	copy(out, m.agents)
	return out
}

// ---------------------------------------------------------------------------
// A: memory host event log scanning
// ---------------------------------------------------------------------------

func (m *Manager) loadState() {
	raw, err := os.ReadFile(filepath.Join(m.cfg.DataDir, "memory-state.json"))
	if err != nil {
		return
	}
	var st persistedState
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	if len(st.Offsets) > 0 {
		m.offsets = st.Offsets
	}
	for agent, counters := range st.Counters {
		if counters != nil {
			m.recalls[agent] = counters
		}
	}
}

func (m *Manager) saveState() {
	st := persistedState{Offsets: m.offsets, Counters: m.recalls}
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	path := filepath.Join(m.cfg.DataDir, "memory-state.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (m *Manager) scanEventLogs() {
	m.mu.RLock()
	agents := m.agentsLocked()
	m.mu.RUnlock()
	for _, a := range agents {
		path := filepath.Join(a.WorkspaceDir, "memory", ".dreams", "events.jsonl")
		m.scanEventLog(a.Name, path)
	}
	m.saveState()
}

func (m *Manager) scanEventLog(agent, path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	cur := agentOffset{Path: path, Ino: infoIno(info), Size: info.Size()}
	m.mu.RLock()
	prev, ok := m.offsets[agent]
	m.mu.RUnlock()
	start := int64(0)
	if ok && prev.Path == path && prev.Ino == cur.Ino && prev.Size <= cur.Size {
		start = prev.Size
	}
	if cur.Size > start {
		if err := m.readEventLog(path, start, agent); err != nil && m.log != nil {
			m.log.Warn("scan memory host event log", "agent", agent, "path", path, "error", err)
		}
	}
	m.mu.Lock()
	m.offsets[agent] = cur
	m.mu.Unlock()
}

func (m *Manager) readEventLog(path string, offset int64, agent string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}
	st := &recallStats{DreamPhases: map[string]uint64{}}
	scanner := newScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := m.applyEventLine(st, line); err != nil {
			// Corrupt partial lines are expected while a write is in flight.
			continue
		}
	}
	m.mergeRecalls(agent, st)
	return scanner.Err()
}

func (m *Manager) mergeRecalls(agent string, delta *recallStats) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.recalls[agent]
	if cur == nil {
		cur = &recallStats{DreamPhases: map[string]uint64{}}
		m.recalls[agent] = cur
	}
	if cur.DreamPhases == nil {
		cur.DreamPhases = map[string]uint64{}
	}
	cur.Recalls += delta.Recalls
	cur.Hits += delta.Hits
	cur.Misses += delta.Misses
	cur.Skipped += delta.Skipped
	cur.ActiveTurns += delta.ActiveTurns
	cur.BackgroundTurns += delta.BackgroundTurns
	cur.Promotions += delta.Promotions
	cur.PromotionApplied += delta.PromotionApplied
	cur.DreamsFailed += delta.DreamsFailed
	cur.DreamLines += delta.DreamLines
	for phase, n := range delta.DreamPhases {
		cur.DreamPhases[phase] += n
	}
}

func (m *Manager) applyEventLine(st *recallStats, line []byte) error {
	var rec map[string]any
	if err := json.Unmarshal(line, &rec); err != nil {
		return err
	}
	typ, _ := rec["type"].(string)
	switch typ {
	case "memory.recall.recorded":
		st.Recalls++
		if number(rec["resultCount"]) > 0 {
			st.Hits++
		} else {
			st.Misses++
		}
		m.tallyTurn(st, rec["query"])
	case "memory.recall.skipped":
		st.Recalls++
		st.Skipped++
		m.tallyTurn(st, rec["query"])
	case "memory.promotion.applied":
		st.Promotions++
		st.PromotionApplied += uint64(number(rec["applied"]))
	case "memory.dream.completed":
		phase := stringValue(rec["phase"])
		if phase == "" {
			phase = "completed"
		}
		if st.DreamPhases == nil {
			st.DreamPhases = map[string]uint64{}
		}
		st.DreamPhases[phase]++
		if stringValue(rec["outcome"]) == "failed" {
			st.DreamsFailed++
		}
		st.DreamLines += uint64(number(rec["lineCount"]))
	}
	return nil
}

func (m *Manager) tallyTurn(st *recallStats, query any) {
	q := stringValue(query)
	if strings.HasPrefix(q, "__dreaming_") {
		st.BackgroundTurns++
	} else {
		st.ActiveTurns++
	}
}

// ---------------------------------------------------------------------------
// C: memory index health collection
// ---------------------------------------------------------------------------

func (m *Manager) collectIndex() {
	m.mu.RLock()
	agents := m.agentsLocked()
	prev := make(map[string]IndexSnapshot, len(m.index))
	for k, v := range m.index {
		prev[k] = v
	}
	m.mu.RUnlock()
	if len(agents) == 0 {
		return
	}

	// Serve the fast SQLite snapshot immediately; enrich with CLI status (files,
	// chunks, dirty) asynchronously because `openclaw memory status` can take a
	// while in the daemon context.
	updated := map[string]IndexSnapshot{}
	for _, a := range agents {
		updated[a.Name] = m.collectIndexSQLite(a, prev[a.Name])
	}
	m.mu.Lock()
	m.agents = agents
	for k, v := range updated {
		m.index[k] = v
		if _, ok := m.recalls[k]; !ok {
			m.recalls[k] = &recallStats{DreamPhases: map[string]uint64{}}
		}
	}
	m.mu.Unlock()
	m.applyCLIStatus(agents)
}

// applyCLIStatus fetches `openclaw memory status --json` offline and enriches
// the latest index snapshot with authoritative files/chunks/dirty values. Runs
// in a background goroutine so a slow CLI never blocks event-log scanning.
func (m *Manager) applyCLIStatus(agents []Agent) {
	started := time.Now()
	statuses, err := m.fetchCLIStatus()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.log.Warn("openclaw memory status unavailable; keeping SQLite snapshot", "error", err, "duration_s", time.Since(started).Seconds())
		return
	}
	byStatus := map[string]cliAgentStatus{}
	for _, s := range statuses {
		byStatus[s.AgentID] = s
	}
	if len(byStatus) == 0 {
		m.log.Warn("openclaw memory status returned no agents; keeping SQLite snapshot")
		return
	}
	for _, a := range agents {
		cs, ok := byStatus[a.Name]
		if !ok {
			continue
		}
		snap := m.index[a.Name]
		if snap.Agent == "" {
			snap.Agent = a.Name
			snap.SampledAt = time.Now().UTC()
		}
		snap.Files = cs.Status.Files
		snap.Chunks = cs.Status.Chunks
		dirty := cs.Status.Dirty
		snap.Dirty = &dirty
		snap.State = 2
		m.index[a.Name] = snap
	}
	m.log.Info("openclaw memory status applied", "agents", len(byStatus), "duration_s", time.Since(started).Seconds())
}

func (m *Manager) fetchCLIStatus() ([]cliAgentStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.CLITimeout)
	defer cancel()
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	// Run under /usr/bin/env -i with a curated environment. Launchd injects an
	// environment that can break the node-based openclaw shim; a controlled
	// environment (verified with env -i) is reliable.
	cmd := exec.CommandContext(ctx, "/usr/bin/env", "-i",
		"PATH=/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME="+home,
		"USER="+os.Getenv("USER"),
		"LOGNAME="+os.Getenv("LOGNAME"),
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"TMPDIR=/tmp",
		m.cfg.OpenCLawBin, "memory", "status", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	var statuses []cliAgentStatus
	if err := json.Unmarshal(out, &statuses); err != nil {
		return nil, err
	}
	return statuses, nil
}

func (m *Manager) collectIndexSQLite(a Agent, prev IndexSnapshot) IndexSnapshot {
	snap := prev
	snap.Agent = a.Name
	snap.SampledAt = time.Now().UTC()
	if a.DBPath == "" {
		snap.State = 0
		return snap
	}
	db, err := openReadOnly(a.DBPath)
	if err != nil {
		snap.State = 0
		return snap
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var revision, pageCount, freelist int64
	var maxUpdated int64
	var files, chunks int64
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_index_sources`).Scan(&files)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_index_chunks`).Scan(&chunks)
	_ = db.QueryRowContext(ctx, `SELECT revision FROM memory_index_state WHERE id=1`).Scan(&revision)
	_ = db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pageCount)
	_ = db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freelist)
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(updated_at),0) FROM memory_index_chunks`).Scan(&maxUpdated)

	snap.Files = files
	snap.Chunks = chunks
	snap.Revision = revision
	snap.PageCount = pageCount
	snap.FreeListPages = freelist
	if maxUpdated > 0 {
		snap.LastIndexedAt = time.UnixMilli(maxUpdated).UTC()
		snap.HasIndex = true
	}
	if fi, err := os.Stat(a.DBPath); err == nil {
		snap.DBSizeBytes = fi.Size()
	}
	if fi, err := os.Stat(a.DBPath + "-wal"); err == nil {
		snap.WALBytes = fi.Size()
	}
	if snap.State == 0 {
		snap.State = 1
	}
	return snap
}

// ---------------------------------------------------------------------------
// B: LM Studio / embedding provider probe
// ---------------------------------------------------------------------------

func (m *Manager) probeProvider() {
	previous := m.probeSnapshot()
	started := time.Now()
	statusCode, modelCount, err := m.once()
	up := err == nil && statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
	m.recordProbe(time.Since(started), statusCode, modelCount, err)
	if !up && previous.ConsecutiveFailures == 0 {
		m.log.Warn("embedding provider probe failed", "url", m.cfg.ProbeURL, "status_code", statusCode, "error", err)
	} else if up && previous.ConsecutiveFailures > 0 {
		m.log.Info("embedding provider probe recovered", "url", m.cfg.ProbeURL, "duration", time.Since(started))
	}
}

// once performs a probe. The direct Go HTTP client is preferred; when the OS
// blocks the daemon's sockets to LAN addresses (macOS local-network privacy),
// it falls back to /usr/bin/curl, which is permitted in the launchd context.
func (m *Manager) once() (statusCode int, modelCount int64, err error) {
	req, reqErr := http.NewRequest(http.MethodGet, m.cfg.ProbeURL, nil)
	if reqErr != nil {
		return 0, 0, reqErr
	}
	resp, doErr := m.client.Do(req)
	if doErr == nil {
		statusCode = resp.StatusCode
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return statusCode, parseModelCount(body), nil
	}
	if m.cfg.CurlBin == "" {
		return 0, 0, doErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.ProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.cfg.CurlBin, "-sS", "--max-time", fmt.Sprintf("%.0f", m.cfg.ProbeTimeout.Seconds()), "-w", "\n__HTTP_CODE__%{http_code}", m.cfg.ProbeURL)
	out, curlErr := cmd.Output()
	if curlErr != nil {
		return 0, 0, fmt.Errorf("direct: %v; curl fallback: %w", doErr, curlErr)
	}
	body := out
	var code int
	if i := bytes.LastIndex(out, []byte("__HTTP_CODE__")); i >= 0 {
		code, _ = strconv.Atoi(strings.TrimSpace(string(out[i+len("__HTTP_CODE__"):])))
		body = out[:i]
	}
	if code == 0 {
		return 0, 0, fmt.Errorf("direct: %v; curl fallback returned no status", doErr)
	}
	return code, parseModelCount(body), nil
}

func parseModelCount(body []byte) int64 {
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		return int64(len(envelope.Data))
	}
	var models []json.RawMessage
	if err := json.Unmarshal(body, &models); err == nil {
		return int64(len(models))
	}
	return 0
}

func (m *Manager) recordProbe(duration time.Duration, statusCode int, modelCount int64, err error) {
	now := time.Now().UTC()
	up := err == nil && statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
	m.mu.Lock()
	defer m.mu.Unlock()
	m.probe.Up = up
	m.probe.DurationSeconds = duration.Seconds()
	m.probe.ModelCount = modelCount
	m.probe.LastProbedAt = &now
	if up {
		m.probe.ConsecutiveFailures = 0
		m.probe.LastSuccessAt = &now
	} else {
		m.probe.ConsecutiveFailures++
	}
}

func (m *Manager) probeSnapshot() EmbeddingProbeSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.probe
}

// ---------------------------------------------------------------------------
// Metric exports
// ---------------------------------------------------------------------------

func (m *Manager) MemoryMetrics() []MetricGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var groups []MetricGroup
	add := func(name, typ, help string, fn func(*recallStats) float64) {
		rows := []storage.MetricRow{}
		for _, a := range m.agents {
			st := m.recalls[a.Name]
			if st == nil {
				st = &recallStats{DreamPhases: map[string]uint64{}}
			}
			rows = append(rows, storage.MetricRow{Labels: map[string]string{"agentId": a.Name}, Value: fn(st)})
		}
		groups = append(groups, MetricGroup{Name: name, Type: typ, Help: help, Rows: rows})
	}
	add("openclaw_memory_recall_calls_total", "counter", "Total memory recall attempts recorded by the memory host event log (recorded + skipped).", func(st *recallStats) float64 { return float64(st.Recalls) })
	add("openclaw_memory_recall_hits_total", "counter", "Memory recalls that returned at least one result.", func(st *recallStats) float64 { return float64(st.Hits) })
	add("openclaw_memory_recall_misses_total", "counter", "Memory recalls that returned zero results (no relevant memory).", func(st *recallStats) float64 { return float64(st.Misses) })
	add("openclaw_memory_recall_skipped_total", "counter", "Memory recalls where hits were visible but excluded from short-term promotion.", func(st *recallStats) float64 { return float64(st.Skipped) })
	add("openclaw_memory_recall_turns_total", "counter", "Memory recall attempts by source; active recall turns (runtime) approximate eligible turns, background turns are dreaming scans.", func(st *recallStats) float64 {
		return float64(st.ActiveTurns + st.BackgroundTurns)
	})
	activeRows := []storage.MetricRow{}
	backgroundRows := []storage.MetricRow{}
	for _, a := range m.agents {
		st := m.recalls[a.Name]
		if st == nil {
			st = &recallStats{DreamPhases: map[string]uint64{}}
		}
		activeRows = append(activeRows, storage.MetricRow{Labels: map[string]string{"agentId": a.Name, "source": "active"}, Value: float64(st.ActiveTurns)})
		backgroundRows = append(backgroundRows, storage.MetricRow{Labels: map[string]string{"agentId": a.Name, "source": "background"}, Value: float64(st.BackgroundTurns)})
	}
	groups = append(groups, MetricGroup{Name: "openclaw_memory_recall_turns_by_source_total", Type: "counter", Help: "Memory recall attempts by source (active runtime vs background dreaming).", Rows: append(activeRows, backgroundRows...)})
	add("openclaw_memory_promotions_total", "counter", "Memory promotion events applied.", func(st *recallStats) float64 { return float64(st.Promotions) })
	add("openclaw_memory_promotion_entries_total", "counter", "Total durable-memory entries promoted.", func(st *recallStats) float64 { return float64(st.PromotionApplied) })
	dreamRows := []storage.MetricRow{}
	for _, a := range m.agents {
		st := m.recalls[a.Name]
		if st == nil {
			st = &recallStats{DreamPhases: map[string]uint64{}}
		}
		for phase, n := range st.DreamPhases {
			dreamRows = append(dreamRows, storage.MetricRow{Labels: map[string]string{"agentId": a.Name, "phase": phase}, Value: float64(n)})
		}
	}
	groups = append(groups, MetricGroup{Name: "openclaw_memory_dreams_total", Type: "counter", Help: "Memory dreaming phase completions by phase.", Rows: dreamRows})
	add("openclaw_memory_dream_lines_total", "counter", "Total lines written by dreaming reports.", func(st *recallStats) float64 { return float64(st.DreamLines) })
	add("openclaw_memory_dreams_failed_total", "counter", "Memory dreaming phases that failed.", func(st *recallStats) float64 { return float64(st.DreamsFailed) })
	return groups
}

func (m *Manager) IndexMetrics() []MetricGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var groups []MetricGroup
	add := func(name, help string, fn func(IndexSnapshot) storage.MetricRow) {
		rows := []storage.MetricRow{}
		for _, a := range m.agents {
			if snap, ok := m.index[a.Name]; ok {
				rows = append(rows, fn(snap))
			}
		}
		groups = append(groups, MetricGroup{Name: name, Type: "gauge", Help: help, Rows: rows})
	}
	add("openclaw_memory_index_files", "Number of files in the agent's memory index.", func(s IndexSnapshot) storage.MetricRow {
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: float64(s.Files)}
	})
	add("openclaw_memory_index_chunks", "Number of indexed chunks in the agent's memory index.", func(s IndexSnapshot) storage.MetricRow {
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: float64(s.Chunks)}
	})
	add("openclaw_memory_index_revision", "Memory index revision counter from the agent's index state.", func(s IndexSnapshot) storage.MetricRow {
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: float64(s.Revision)}
	})
	add("openclaw_memory_index_db_bytes", "SQLite memory index database file size in bytes.", func(s IndexSnapshot) storage.MetricRow {
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: float64(s.DBSizeBytes)}
	})
	add("openclaw_memory_index_db_wal_bytes", "SQLite memory index WAL file size in bytes.", func(s IndexSnapshot) storage.MetricRow {
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: float64(s.WALBytes)}
	})
	add("openclaw_memory_index_freelist_ratio", "Ratio of free pages to total pages in the memory index database.", func(s IndexSnapshot) storage.MetricRow {
		ratio := 0.0
		if s.PageCount > 0 {
			ratio = float64(s.FreeListPages) / float64(s.PageCount)
		}
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: ratio}
	})
	add("openclaw_memory_index_freshness_seconds", "Age of the most recently indexed chunk in the agent's memory index.", func(s IndexSnapshot) storage.MetricRow {
		fresh := 0.0
		if s.HasIndex {
			fresh = time.Since(s.LastIndexedAt).Seconds()
			if fresh < 0 {
				fresh = 0
			}
		}
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: fresh}
	})
	add("openclaw_memory_index_last_indexed_unixtime", "Unix timestamp of the most recently indexed memory chunk.", func(s IndexSnapshot) storage.MetricRow {
		v := 0.0
		if s.HasIndex {
			v = float64(s.LastIndexedAt.Unix())
		}
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: v}
	})
	add("openclaw_memory_index_state", "Memory index collection state: 2=CLI status, 1=SQLite fallback, 0=unavailable.", func(s IndexSnapshot) storage.MetricRow {
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: float64(s.State)}
	})
	add("openclaw_memory_index_sampled_at_unixtime", "Unix timestamp of the most recent memory index sample.", func(s IndexSnapshot) storage.MetricRow {
		return storage.MetricRow{Labels: map[string]string{"agentId": s.Agent}, Value: float64(s.SampledAt.Unix())}
	})
	dirtyRows := []storage.MetricRow{}
	for _, a := range m.agents {
		snap, ok := m.index[a.Name]
		if !ok || snap.Dirty == nil {
			continue
		}
		v := 0.0
		if *snap.Dirty {
			v = 1
		}
		dirtyRows = append(dirtyRows, storage.MetricRow{Labels: map[string]string{"agentId": a.Name}, Value: v})
	}
	groups = append(groups, MetricGroup{Name: "openclaw_memory_index_dirty", Type: "gauge", Help: "Whether the agent's memory index is dirty (needs reindexing).", Rows: dirtyRows})
	return groups
}

func (m *Manager) ProbeMetrics() []MetricGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()
	labels := func() map[string]string {
		return map[string]string{"provider": "lmstudio", "url": m.cfg.ProbeURL}
	}
	lastSuccess := float64(0)
	if m.probe.LastSuccessAt != nil {
		lastSuccess = float64(m.probe.LastSuccessAt.Unix())
	}
	up := 0.0
	if m.probe.Up {
		up = 1
	}
	return []MetricGroup{
		{Name: "openclaw_embedding_provider_up", Type: "gauge", Help: "Whether the embedding provider endpoint is reachable.", Rows: []storage.MetricRow{{Labels: labels(), Value: up}}},
		{Name: "openclaw_embedding_provider_models", Type: "gauge", Help: "Number of models advertised by the embedding provider.", Rows: []storage.MetricRow{{Labels: labels(), Value: float64(m.probe.ModelCount)}}},
		{Name: "openclaw_embedding_provider_probe_duration_seconds", Type: "gauge", Help: "Duration of the latest embedding provider probe.", Rows: []storage.MetricRow{{Labels: labels(), Value: m.probe.DurationSeconds}}},
		{Name: "openclaw_embedding_provider_probe_consecutive_failures", Type: "gauge", Help: "Consecutive failed embedding provider probes.", Rows: []storage.MetricRow{{Labels: labels(), Value: float64(m.probe.ConsecutiveFailures)}}},
		{Name: "openclaw_embedding_provider_probe_last_success_unixtime", Type: "gauge", Help: "Unix timestamp of the latest successful embedding provider probe.", Rows: []storage.MetricRow{{Labels: labels(), Value: lastSuccess}}},
	}
}

// Snapshot builds the lightweight in-memory view for the /api/v1/memory
// endpoint. It never touches SQLite; recall counters and index health are read
// under the manager lock, so it is safe to poll frequently.
func (m *Manager) Snapshot() MemorySnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now().UTC()
	snap := MemorySnapshot{GeneratedAt: now, Agents: make([]AgentMemorySnapshot, 0, len(m.agents))}
	for _, a := range m.agents {
		st := m.recalls[a.Name]
		if st == nil {
			st = &recallStats{DreamPhases: map[string]uint64{}}
		}
		phases := make(map[string]uint64, len(st.DreamPhases))
		for phase, n := range st.DreamPhases {
			phases[phase] = n
		}
		agent := AgentMemorySnapshot{
			Name: a.Name, Recalls: st.Recalls, Hits: st.Hits, Misses: st.Misses, Skipped: st.Skipped,
			ActiveTurns: st.ActiveTurns, BackgroundTurns: st.BackgroundTurns,
			Promotions: st.Promotions, PromotionEntries: st.PromotionApplied,
			DreamLines: st.DreamLines, DreamsFailed: st.DreamsFailed, DreamPhases: phases,
		}
		if idx, ok := m.index[a.Name]; ok {
			agent.IndexFiles = idx.Files
			agent.IndexChunks = idx.Chunks
			agent.IndexRevision = idx.Revision
			agent.IndexDBBytes = idx.DBSizeBytes
			agent.IndexWALBytes = idx.WALBytes
			if idx.PageCount > 0 {
				agent.IndexFreelist = float64(idx.FreeListPages) / float64(idx.PageCount)
			}
			if idx.HasIndex {
				agent.IndexLastIndexed = float64(idx.LastIndexedAt.Unix())
				fresh := now.Sub(idx.LastIndexedAt).Seconds()
				if fresh < 0 {
					fresh = 0
				}
				agent.IndexFreshness = fresh
			}
			agent.IndexState = idx.State
			agent.IndexSampledAt = float64(idx.SampledAt.Unix())
			if idx.Dirty != nil {
				agent.IndexDirty = *idx.Dirty
				agent.IndexDirtyKnown = true
			}
		}
		snap.Agents = append(snap.Agents, agent)
	}
	probe := m.probe
	snap.Provider.Up = probe.Up
	snap.Provider.ModelCount = probe.ModelCount
	snap.Provider.ProbeDurationSeconds = probe.DurationSeconds
	snap.Provider.ConsecutiveFailures = probe.ConsecutiveFailures
	if probe.LastSuccessAt != nil {
		snap.Provider.LastSuccessAtUnix = float64(probe.LastSuccessAt.Unix())
	}
	return snap
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func Quantiles(values []float64) (p50, p95 float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return percentile(sorted, 0.50), percentile(sorted, 0.95)
}

func percentile(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 1 {
		return sorted[0]
	}
	pos := q * float64(n-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= n {
		return sorted[n-1]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func openReadOnly(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err == nil {
		db.SetMaxOpenConns(1)
		return db, nil
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func infoIno(info os.FileInfo) uint64 {
	switch st := info.Sys().(type) {
	case *syscall.Stat_t:
		return uint64(st.Ino)
	}
	return 0
}

func newScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 2*1024*1024)
	return sc
}