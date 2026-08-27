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
//
// Rollups300Days / Rollups3600Days – retention windows for trend_rollups rows
// by bucket size (300s / 3600s). 0 means "do not purge trend_rollups".
type RetentionConfig struct {
	RawEventsDays   int
	SamplesDays     int
	AllDays         int
	Rollups300Days  int
	Rollups3600Days int
}

func (c RetentionConfig) normalized() RetentionConfig {
	if c.RawEventsDays <= 0 {
		c.RawEventsDays = 7
	}
	if c.SamplesDays <= 0 {
		c.SamplesDays = 30
	}
	if c.Rollups300Days < 0 {
		c.Rollups300Days = 0
	}
	if c.Rollups3600Days < 0 {
		c.Rollups3600Days = 0
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

	// 4. Prune trend_rollups by bucket size. Each bucket size has its own
	// retention window (300s → 30 days, 3600s → 90 days). Only the rollup
	// buckets are removed; the trend_rollup_watermarks watermarks are never
	// deleted and their covered_from is only ever advanced forward, so a
	// rebuild that starts from covered_from stays inside the retention window.
	if cfg.Rollups300Days > 0 {
		before := cutoff.AddDate(0, 0, -cfg.Rollups300Days)
		deleted, err := j.repo.pruneTrendRollups(ctx, 300, before)
		if err != nil {
			j.log.Error("retention: purge trend_rollups", "bucket_seconds", 300, "error", err)
		} else if deleted > 0 {
			j.log.Info("retention: purged old trend_rollups", "bucket_seconds", 300, "rows", deleted, "cutoff", before.Format(time.RFC3339))
		}
	}
	if cfg.Rollups3600Days > 0 {
		before := cutoff.AddDate(0, 0, -cfg.Rollups3600Days)
		deleted, err := j.repo.pruneTrendRollups(ctx, 3600, before)
		if err != nil {
			j.log.Error("retention: purge trend_rollups", "bucket_seconds", 3600, "error", err)
		} else if deleted > 0 {
			j.log.Info("retention: purged old trend_rollups", "bucket_seconds", 3600, "rows", deleted, "cutoff", before.Format(time.RFC3339))
		}
	}
}

// trendRollupBuckets are the bucket sizes the retention job knows how to
// prune. Anything else is rejected to prevent accidental full-table deletes.
var trendRollupBuckets = map[int]bool{300: true, 3600: true}

// pruneTrendRollups removes trend_rollups rows for a given bucket size whose
// bucket_start predates the cutoff. Rows are deleted in bounded batches so the
// SQLite write lock is not held for an unbounded delete.
//
// The trend_rollup_watermarks rows are intentionally left in place. Their
// covered_from is advanced to the retention window start (never moved back),
// which constrains any covered_from-based rebuild to the retained window so
// pruned buckets are not recreated. If the table is absent (e.g. the rollup
// builder is not deployed), this is a no-op.
func (r *Repository) pruneTrendRollups(ctx context.Context, bucketSeconds int, cutoff time.Time) (int64, error) {
	if !trendRollupBuckets[bucketSeconds] {
		return 0, fmt.Errorf("retention: unsupported trend_rollups bucket_seconds %d", bucketSeconds)
	}
	exists, err := r.tableExists(ctx, "trend_rollups")
	if err != nil || !exists {
		return 0, err
	}
	query := `DELETE FROM trend_rollups WHERE rowid IN (
  SELECT rowid FROM trend_rollups WHERE bucket_seconds=? AND bucket_start < ? LIMIT ?
)`
	var deleted int64
	for {
		res, err := r.db.ExecContext(ctx, query, bucketSeconds, cutoff.Format(time.RFC3339Nano), retentionDeleteBatchSize)
		if err != nil {
			return deleted, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return deleted, err
		}
		deleted += n
		if n < retentionDeleteBatchSize {
			break
		}
	}
	if _, err := r.advanceTrendRollupWatermarks(ctx, bucketSeconds, cutoff); err != nil {
		return deleted, err
	}
	return deleted, nil
}

// advanceTrendRollupWatermarks moves each watermark's covered_from forward so
// it never predates the retention window. This is the "rebuild start takes
// max(watermark, retention window start)" guard applied at the data layer:
// a builder that (re)builds from covered_from will not resurrect pruned
// buckets because the surviving window already starts at the cutoff.
func (r *Repository) advanceTrendRollupWatermarks(ctx context.Context, bucketSeconds int, minCoveredFrom time.Time) (int64, error) {
	exists, err := r.tableExists(ctx, "trend_rollup_watermarks")
	if err != nil || !exists {
		return 0, err
	}
	cutoff := minCoveredFrom.Format(time.RFC3339Nano)
	res, err := r.db.ExecContext(ctx, `UPDATE trend_rollup_watermarks
	  SET covered_from=?
	  WHERE bucket_seconds=? AND covered_from<>'' AND covered_from<?`,
		cutoff, bucketSeconds, cutoff)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (r *Repository) tableExists(ctx context.Context, table string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n)
	return n > 0, err
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

	if cfg.Rollups300Days > 0 {
		before := cutoff.AddDate(0, 0, -cfg.Rollups300Days)
		n, err := r.pruneTrendRollups(ctx, 300, before)
		if err != nil {
			return results, fmt.Errorf("trend_rollups_300: %w", err)
		}
		results["trend_rollups_300"] = n
	}

	if cfg.Rollups3600Days > 0 {
		before := cutoff.AddDate(0, 0, -cfg.Rollups3600Days)
		n, err := r.pruneTrendRollups(ctx, 3600, before)
		if err != nil {
			return results, fmt.Errorf("trend_rollups_3600: %w", err)
		}
		results["trend_rollups_3600"] = n
	}

	return results, nil
}
