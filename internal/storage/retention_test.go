package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// trendRollupTestDDL mirrors the trend_rollups schema used by the rollup
// builder at runtime so the retention purge can be exercised in isolation.
const trendRollupTestDDL = `
CREATE TABLE IF NOT EXISTS trend_rollups (
  bucket_seconds INTEGER NOT NULL,
  bucket_start TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  series TEXT NOT NULL,
  dimension_key TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  PRIMARY KEY(bucket_seconds, instance_id, series, bucket_start, dimension_key)
);
CREATE INDEX IF NOT EXISTS idx_trend_rollups_refresh
  ON trend_rollups(bucket_seconds, bucket_start, instance_id, series);
CREATE TABLE IF NOT EXISTS trend_rollup_watermarks (
  bucket_seconds INTEGER NOT NULL,
  instance_id TEXT NOT NULL,
  covered_from TEXT NOT NULL,
  covered_to TEXT NOT NULL,
  refreshed_at TEXT NOT NULL,
  PRIMARY KEY(bucket_seconds, instance_id)
);
`

func openRetentionRepo(t *testing.T) *Repository {
	t.Helper()
	repo, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	if _, err := repo.db.Exec(trendRollupTestDDL); err != nil {
		t.Fatal(err)
	}
	return repo
}

func insertTrendRollup(t *testing.T, repo *Repository, bucketSeconds int, bucketStart string) {
	t.Helper()
	_, err := repo.db.Exec(`INSERT INTO trend_rollups(bucket_seconds,bucket_start,instance_id,series,dimension_key,payload_json)
	  VALUES(?,?,?,?,?,?)`, bucketSeconds, bucketStart, "inst-a", "series-a", "all::all", `{"v":1}`)
	if err != nil {
		t.Fatal(err)
	}
}

func insertTrendRollupWatermark(t *testing.T, repo *Repository, bucketSeconds int, instanceID, coveredFrom, coveredTo string) {
	t.Helper()
	_, err := repo.db.Exec(`INSERT OR REPLACE INTO trend_rollup_watermarks(bucket_seconds,instance_id,covered_from,covered_to,refreshed_at)
	  VALUES(?,?,?,?,?)`, bucketSeconds, instanceID, coveredFrom, coveredTo, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
}

func countTrendRollups(t *testing.T, repo *Repository, bucketSeconds int) int64 {
	t.Helper()
	var n int64
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM trend_rollups WHERE bucket_seconds=?`, bucketSeconds).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPruneTrendRollupsDeletesOnlyExpiredBucketsPerWindow(t *testing.T) {
	repo := openRetentionRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old300 := now.AddDate(0, 0, -40).Format(time.RFC3339Nano)  // outside 30d window
	keep300 := now.AddDate(0, 0, -1).Format(time.RFC3339Nano)   // inside 30d window
	old3600 := now.AddDate(0, 0, -100).Format(time.RFC3339Nano) // outside 90d window
	keep3600 := now.AddDate(0, 0, -10).Format(time.RFC3339Nano) // inside 90d window

	insertTrendRollup(t, repo, 300, old300)
	insertTrendRollup(t, repo, 300, keep300)
	insertTrendRollup(t, repo, 3600, old3600)
	insertTrendRollup(t, repo, 3600, keep3600)

	cutoff300 := now.AddDate(0, 0, -30)
	deleted300, err := repo.pruneTrendRollups(ctx, 300, cutoff300)
	if err != nil {
		t.Fatal(err)
	}
	if deleted300 != 1 {
		t.Fatalf("pruned %d rows for bucket 300, want 1", deleted300)
	}
	cutoff3600 := now.AddDate(0, 0, -90)
	deleted3600, err := repo.pruneTrendRollups(ctx, 3600, cutoff3600)
	if err != nil {
		t.Fatal(err)
	}
	if deleted3600 != 1 {
		t.Fatalf("pruned %d rows for bucket 3600, want 1", deleted3600)
	}

	var remaining300 int64
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM trend_rollups WHERE bucket_seconds=300 AND bucket_start=?`, keep300).Scan(&remaining300); err != nil {
		t.Fatal(err)
	}
	if remaining300 != 1 {
		t.Fatalf("recent bucket 300 row was pruned")
	}
	var remaining3600 int64
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM trend_rollups WHERE bucket_seconds=3600 AND bucket_start=?`, keep3600).Scan(&remaining3600); err != nil {
		t.Fatal(err)
	}
	if remaining3600 != 1 {
		t.Fatalf("recent bucket 3600 row was pruned")
	}
}

func TestPruneTrendRollupsAdvancesWatermarkForwardAndKeepsRows(t *testing.T) {
	repo := openRetentionRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	coveredFrom := now.AddDate(0, 0, -120).Format(time.RFC3339Nano)
	coveredTo := now.Format(time.RFC3339Nano)
	insertTrendRollupWatermark(t, repo, 300, "inst-a", coveredFrom, coveredTo)
	insertTrendRollupWatermark(t, repo, 300, "inst-b", coveredFrom, coveredTo)

	cutoff := now.AddDate(0, 0, -30)
	if _, err := repo.pruneTrendRollups(ctx, 300, cutoff); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM trend_rollup_watermarks`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("watermark rows were deleted: count=%d, want 2", rows)
	}

	var updated int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM trend_rollup_watermarks WHERE bucket_seconds=300 AND covered_from=?`, cutoff.Format(time.RFC3339Nano)).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if updated != 2 {
		t.Fatalf("watermark covered_from not advanced to retention window: updated=%d, want 2", updated)
	}

	var keptTo int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM trend_rollup_watermarks WHERE covered_to=?`, coveredTo).Scan(&keptTo); err != nil {
		t.Fatal(err)
	}
	if keptTo != 2 {
		t.Fatalf("watermark covered_to regressed: %d", keptTo)
	}
}

func TestRunRetentionOncePrunesTrendRollupsAndSkipsWhenDisabled(t *testing.T) {
	repo := openRetentionRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTrendRollup(t, repo, 300, now.AddDate(0, 0, -40).Format(time.RFC3339Nano))
	insertTrendRollup(t, repo, 300, now.AddDate(0, 0, -2).Format(time.RFC3339Nano))
	insertTrendRollup(t, repo, 3600, now.AddDate(0, 0, -95).Format(time.RFC3339Nano))
	insertTrendRollup(t, repo, 3600, now.AddDate(0, 0, -5).Format(time.RFC3339Nano))
	insertTrendRollupWatermark(t, repo, 300, "inst-a", now.AddDate(0, 0, -100).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	insertTrendRollupWatermark(t, repo, 3600, "inst-a", now.AddDate(0, 0, -200).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))

	res, err := repo.RunRetentionOnce(ctx, RetentionConfig{Rollups300Days: 30, Rollups3600Days: 90})
	if err != nil {
		t.Fatal(err)
	}
	if res["trend_rollups_300"] != 1 || res["trend_rollups_3600"] != 1 {
		t.Fatalf("unexpected rollup prune counts: %#v", res)
	}
	if n := countTrendRollups(t, repo, 300); n != 1 {
		t.Fatalf("bucket 300 retained %d rows after prune, want 1", n)
	}
	if n := countTrendRollups(t, repo, 3600); n != 1 {
		t.Fatalf("bucket 3600 retained %d rows after prune, want 1", n)
	}

	// Disabled (0) must leave everything intact, including untouched watermarks.
	before := countTrendRollups(t, repo, 300) + countTrendRollups(t, repo, 3600)
	res2, err := repo.RunRetentionOnce(ctx, RetentionConfig{Rollups300Days: 0, Rollups3600Days: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res2["trend_rollups_300"]; ok {
		t.Fatalf("disabled rollup retention still reported 300 prune: %#v", res2)
	}
	if after := countTrendRollups(t, repo, 300) + countTrendRollups(t, repo, 3600); after != before {
		t.Fatalf("disabled rollup retention modified rows: before=%d after=%d", before, after)
	}
}

func TestPruneTrendRollupsRejectsUnknownBucketAndSkipsMissingTable(t *testing.T) {
	repo := openRetentionRepo(t)
	ctx := context.Background()
	if _, err := repo.pruneTrendRollups(ctx, 60, time.Now().UTC()); err == nil {
		t.Fatalf("expected unsupported bucket error, got nil")
	}

	// Fresh repo with no rollup tables: prune must be a silent no-op.
	plain, err := Open(filepath.Join(t.TempDir(), "plain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	deleted, err := plain.pruneTrendRollups(ctx, 300, time.Now().UTC())
	if err != nil {
		t.Fatalf("expected no-op on missing table, got %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted on missing table, got %d", deleted)
	}
}

func TestPruneTrendRollupsBoundedBatches(t *testing.T) {
	repo := openRetentionRepo(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -60).Format(time.RFC3339Nano)
	for i := 0; i < retentionDeleteBatchSize+50; i++ {
		key := fmt.Sprintf("%08d", i)
		if _, err := repo.db.Exec(`INSERT INTO trend_rollups(bucket_seconds,bucket_start,instance_id,series,dimension_key,payload_json)
		  VALUES(300,?,?,?,?,?)`, old, "inst-a", key, "s:"+key, `{"v":1}`); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := repo.pruneTrendRollups(ctx, 300, time.Now().UTC().AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != retentionDeleteBatchSize+50 {
		t.Fatalf("deleted %d rows, want %d", deleted, retentionDeleteBatchSize+50)
	}
}