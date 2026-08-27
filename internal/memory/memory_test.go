package memory

import (
	"math"
	"reflect"
	"testing"
)

func TestApplyEventLine(t *testing.T) {
	st := &recallStats{DreamPhases: map[string]uint64{}}
	lines := []string{
		`{"type":"memory.recall.recorded","timestamp":"2026-04-17T19:04:49.547Z","query":"daily summary","resultCount":3,"results":[]}`,
		`{"type":"memory.recall.recorded","timestamp":"2026-04-17T19:04:50Z","query":"__dreaming_daily__:2026-04-16","resultCount":0,"results":[]}`,
		`{"type":"memory.recall.skipped","timestamp":"2026-04-17T19:04:51Z","query":"visible","reason":"non-short-term-memory-path","eligibleResultCount":2,"skippedResultCount":2,"results":[]}`,
		`{"type":"memory.promotion.applied","timestamp":"2026-04-17T19:04:52Z","memoryPath":"memory/MEMORY.md","applied":4,"candidates":[]}`,
		`{"type":"memory.dream.completed","timestamp":"2026-04-17T19:04:53Z","phase":"light","reportPath":"/tmp/r.md","lineCount":12,"storageMode":"separate"}`,
		`{"type":"memory.dream.completed","timestamp":"2026-04-17T19:04:54Z","phase":"rem","outcome":"failed","error":"boom","lineCount":0,"storageMode":"separate"}`,
		`not json`,
	}
	for i, line := range lines {
		err := applyEventLineStub(st, line)
		if i == len(lines)-1 && err == nil {
			t.Fatalf("expected parse error for corrupt line")
		}
	}
	if st.Recalls != 3 {
		t.Fatalf("recalls = %d, want 3", st.Recalls)
	}
	if st.Hits != 1 || st.Misses != 1 {
		t.Fatalf("hits=%d misses=%d, want 1/1", st.Hits, st.Misses)
	}
	if st.Skipped != 1 {
		t.Fatalf("skipped = %d, want 1", st.Skipped)
	}
	if st.ActiveTurns != 2 || st.BackgroundTurns != 1 {
		t.Fatalf("active=%d background=%d, want 2/1", st.ActiveTurns, st.BackgroundTurns)
	}
	if st.Promotions != 1 || st.PromotionApplied != 4 {
		t.Fatalf("promotions=%d applied=%d, want 1/4", st.Promotions, st.PromotionApplied)
	}
	if st.DreamLines != 12 || st.DreamsFailed != 1 {
		t.Fatalf("dream lines=%d failed=%d, want 12/1", st.DreamLines, st.DreamsFailed)
	}
	wantPhases := map[string]uint64{"light": 1, "rem": 1}
	if !reflect.DeepEqual(st.DreamPhases, wantPhases) {
		t.Fatalf("phases = %v, want %v", st.DreamPhases, wantPhases)
	}
}

func TestQuantiles(t *testing.T) {
	values := []float64{0.1, 0.2, 0.5, 1.0, 2.0, 5.0, 8.0, 10.0}
	p50, p95 := Quantiles(values)
	if math.Abs(p50-1.5) > 1e-9 {
		t.Fatalf("p50 = %v, want 1.5", p50)
	}
	expectedP95 := 8.0 + 0.65*(10.0-8.0)
	if math.Abs(p95-expectedP95) > 1e-9 {
		t.Fatalf("p95 = %v, want %v", p95, expectedP95)
	}
	p50, p95 = Quantiles(nil)
	if p50 != 0 || p95 != 0 {
		t.Fatalf("empty quantiles = %v/%v, want 0/0", p50, p95)
	}
}

func applyEventLineStub(st *recallStats, line string) error {
	return (&Manager{}).applyEventLine(st, []byte(line))
}