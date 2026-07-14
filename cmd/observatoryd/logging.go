package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const logRetentionDays = 7

// dailyLogWriter writes to a UTC-dated file and prunes files outside the
// retention window. It is safe for concurrent slog writes.
type dailyLogWriter struct {
	dir    string
	prefix string
	keep   int
	now    func() time.Time

	mu   sync.Mutex
	day  string
	file *os.File
}

func newDailyLogWriter(dir, prefix string, keep int) (*dailyLogWriter, error) {
	w := &dailyLogWriter{dir: dir, prefix: prefix, keep: keep, now: time.Now}
	if err := w.rotateLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *dailyLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateLocked(); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *dailyLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *dailyLogWriter) rotateLocked() error {
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return err
	}
	day := w.now().UTC().Format("2006-01-02")
	if w.file != nil && day == w.day {
		return nil
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(filepath.Join(w.dir, fmt.Sprintf("%s-%s.log", w.prefix, day)), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.file, w.day = file, day
	return w.pruneLocked()
}

func (w *dailyLogWriter) pruneLocked() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	var dates []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, w.prefix+"-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		date := strings.TrimSuffix(strings.TrimPrefix(name, w.prefix+"-"), ".log")
		if _, err := time.Parse("2006-01-02", date); err == nil {
			dates = append(dates, date)
		}
	}
	sort.Strings(dates)
	for len(dates) > w.keep {
		if err := os.Remove(filepath.Join(w.dir, fmt.Sprintf("%s-%s.log", w.prefix, dates[0]))); err != nil && !os.IsNotExist(err) {
			return err
		}
		dates = dates[1:]
	}
	return nil
}

func newLogger(dataDir string) (*dailyLogWriter, io.Writer, error) {
	writer, err := newDailyLogWriter(filepath.Join(dataDir, "logs"), "observatoryd", logRetentionDays)
	if err != nil {
		return nil, nil, err
	}
	return writer, io.MultiWriter(os.Stderr, writer), nil
}
