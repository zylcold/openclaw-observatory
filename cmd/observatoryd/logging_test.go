package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type temporaryNetError struct{}

func (temporaryNetError) Error() string   { return "temporary" }
func (temporaryNetError) Timeout() bool   { return false }
func (temporaryNetError) Temporary() bool { return true }

var _ net.Error = temporaryNetError{}

func TestDailyLogWriterRotatesAndPrunes(t *testing.T) {
	day := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	w, err := newDailyLogWriter(t.TempDir(), "observatoryd", 2)
	if err != nil {
		t.Fatal(err)
	}
	w.now = func() time.Time { return day }
	for i := 0; i < 3; i++ {
		if _, err := w.Write([]byte("entry\n")); err != nil {
			t.Fatal(err)
		}
		day = day.AddDate(0, 0, 1)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(w.dir, "observatoryd-*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected two retained log files, got %v", entries)
	}
	if _, err := os.Stat(filepath.Join(w.dir, "observatoryd-2026-07-10.log")); !os.IsNotExist(err) {
		t.Fatalf("old log should have been removed, err=%v", err)
	}
}

func TestIsTemporaryNetError(t *testing.T) {
	if !isTemporaryNetError(temporaryNetError{}) {
		t.Fatal("expected temporary network error to retry")
	}
	if isTemporaryNetError(errors.New("address already in use")) {
		t.Fatal("non-network bind error must not retry")
	}
}
