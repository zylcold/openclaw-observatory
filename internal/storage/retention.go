package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const retentionDeleteBatchSize = 1000

var retentionColumns = map[string]map[string]bool{
	"events":           {"occurred_at": true},
	"resource_samples": {"sampled_at": true},
	"llm_calls":        {"started_at": true},
	"tool_calls":       {"started_at": true},
	"mcp_calls":        {"started_at": true},
	"subagent_runs":    {"started_at": true},
	"agent_runs":       {"started_at": true},
	"sessions":         {"COALESCE(started_at,ended_at)": true},
	"retry_events":     {"occurred_at": true},
}

// RetentionConfig controls how old data is purged.
//
// RawEventsDays  – rows in `events` older than this are deleted (default 7).
// SamplesDays    – rows in `resource_samples` older than this are deleted.
// AllDays        – hard cap for projection tables (sessions, agent_runs, etc.).
//
//	0 means "do not purge projection tables".
type RetentionConfig struct {
	RawEventsDays int
	SamplesDays   int
	AllDays       int
}

func (c RetentionConfig) normalized() RetentionConfig {
	if c.RawEventsDays <= 0 {
		c.RawEventsDays = 7
	}
	if c.SamplesDays <= 0 {
		c.SamplesDays = 30
	}
	return c
}

// RetentionJob runs periodic cleanup in a background goroutine.
type RetentionJob struct {
	repo   *Repository
	cfg    RetentionConfig
	log    *slog.Logger
	period time.Duration
}

func NewRetentionJob(repo *Repository, cfg RetentionConfig, log *slog.Logger) *RetentionJob {
	if log == nil {
		log = slog.Default()
	}
	return &RetentionJob{
		repo:   repo,
		cfg:    cfg.normalized(),
		log:    log,
		period: 6 * time.Hour,
	}
}

// Start launches the background cleaner. The returned function stops it.
func (j *RetentionJob) Start() func() {
	ctx, cancel := context.WithCancel(context.Background())
	go j.loop(ctx)
	return cancel
}

func (j *RetentionJob) loop(ctx context.Context) {
	// Run once shortly after start.
	j.runOnce(ctx)
	ticker := time.NewTicker(j.period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.runOnce(ctx)
		}
	}
}

func (j *RetentionJob) runOnce(ctx context.Context) {
	cutoff := time.Now().UTC()
	cfg := j.cfg.normalized()

	// 1. Purge raw events older than RawEventsDays.
	if cfg.RawEventsDays > 0 {
		eventsBefore := cutoff.AddDate(0, 0, -cfg.RawEventsDays)
		deleted, err := j.repo.deleteBefore(ctx, "events", "occurred_at", eventsBefore)
		if err != nil {
			j.log.Error("retention: purge events", "error", err)
		} else if deleted > 0 {
			j.log.Info("retention: purged old events", "rows", deleted, "cutoff", eventsBefore.Format(time.RFC3339))
		}
	}

	// 2. Purge resource_samples older than SamplesDays.
	if cfg.SamplesDays > 0 {
		samplesBefore := cutoff.AddDate(0, 0, -cfg.SamplesDays)
		deleted, err := j.repo.deleteBefore(ctx, "resource_samples", "sampled_at", samplesBefore)
		if err != nil {
			j.log.Error("retention: purge resource_samples", "error", err)
		} else if deleted > 0 {
			j.log.Info("retention: purged old resource_samples", "rows", deleted, "cutoff", samplesBefore.Format(time.RFC3339))
		}
	}

	// 3. Optional: purge all projection tables older than AllDays.
	if cfg.AllDays > 0 {
		allBefore := cutoff.AddDate(0, 0, -cfg.AllDays)
		for _, table := range []string{"llm_calls", "tool_calls", "mcp_calls", "subagent_runs", "agent_runs", "sessions", "retry_events"} {
			col := "started_at"
			if table == "sessions" {
				col = "COALESCE(started_at,ended_at)"
			} else if table == "retry_events" {
				col = "occurred_at"
			}
			deleted, err := j.repo.deleteBefore(ctx, table, col, allBefore)
			if err != nil {
				j.log.Error("retention: purge projection", "table", table, "error", err)
			} else if deleted > 0 {
				j.log.Info("retention: purged old rows", "table", table, "rows", deleted)
			}
		}
	}
}

// deleteBefore removes rows in bounded batches so retention does not hold the
// SQLite write lock for an unbounded delete.
func (r *Repository) deleteBefore(ctx context.Context, table, column string, cutoff time.Time) (int64, error) {
	if !retentionColumns[table][column] {
		return 0, fmt.Errorf("retention: unknown table %q", table)
	}
	query := fmt.Sprintf(`DELETE FROM %s WHERE rowid IN (
  SELECT rowid FROM %s WHERE %s < ? LIMIT ?
)`, table, table, column)
	var deleted int64
	for {
		res, err := r.db.ExecContext(ctx, query, cutoff.Format(time.RFC3339Nano), retentionDeleteBatchSize)
		if err != nil {
			return deleted, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return deleted, err
		}
		deleted += n
		if n < retentionDeleteBatchSize {
			return deleted, nil
		}
	}
}

// RunRetentionOnce exposes the purge for ad-hoc / CLI use.
func (r *Repository) RunRetentionOnce(ctx context.Context, cfg RetentionConfig) (map[string]int64, error) {
	cfg = cfg.normalized()
	cutoff := time.Now().UTC()
	results := make(map[string]int64)

	if cfg.RawEventsDays > 0 {
		before := cutoff.AddDate(0, 0, -cfg.RawEventsDays)
		n, err := r.deleteBefore(ctx, "events", "occurred_at", before)
		if err != nil {
			return results, fmt.Errorf("events: %w", err)
		}
		results["events"] = n
	}

	if cfg.SamplesDays > 0 {
		before := cutoff.AddDate(0, 0, -cfg.SamplesDays)
		n, err := r.deleteBefore(ctx, "resource_samples", "sampled_at", before)
		if err != nil {
			return results, fmt.Errorf("resource_samples: %w", err)
		}
		results["resource_samples"] = n
	}

	if cfg.AllDays > 0 {
		before := cutoff.AddDate(0, 0, -cfg.AllDays)
		for _, table := range []string{"llm_calls", "tool_calls", "mcp_calls", "subagent_runs", "agent_runs", "sessions", "retry_events"} {
			col := "started_at"
			if table == "sessions" {
				col = "COALESCE(started_at,ended_at)"
			} else if table == "retry_events" {
				col = "occurred_at"
			}
			n, err := r.deleteBefore(ctx, table, col, before)
			if err != nil {
				return results, fmt.Errorf("%s: %w", table, err)
			}
			results[table] = n
		}
	}

	return results, nil
}
