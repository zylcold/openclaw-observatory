package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/process"
	"github.com/zylcold/openclaw-observatory/internal/server"
	"github.com/zylcold/openclaw-observatory/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("observatoryd stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	defaultDir := filepath.Join(home, ".openclaw-observatory")
	dataDir := flag.String("data-dir", defaultDir, "private runtime data directory")
	socketPath := flag.String("socket", "", "Unix event socket path (default: data-dir/observatory.sock)")
	dbPath := flag.String("db", "", "SQLite path (default: data-dir/observatory.db)")
	listenAddr := flag.String("listen", "127.0.0.1:10087", "REST/SSE/metrics listen address")
	sampleInterval := flag.Duration("sample-interval", 5*time.Second, "process resource sample interval")
	retentionEvents := flag.Int("retention-events-days", 7, "raw events retention period in days (0 = unlimited)")
	retentionSamples := flag.Int("retention-samples-days", 30, "resource_samples retention period in days (0 = unlimited)")
	retentionAll := flag.Int("retention-all-days", 0, "hard cap for projection tables in days (0 = disabled)")
	flag.Parse()
	if *socketPath == "" {
		*socketPath = filepath.Join(*dataDir, "observatory.sock")
	}
	if *dbPath == "" {
		*dbPath = filepath.Join(*dataDir, "observatory.db")
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(*dataDir, 0o700); err != nil {
		return err
	}
	logWriter, logOutput, err := newLogger(*dataDir)
	if err != nil {
		return err
	}
	defer logWriter.Close()
	logger := slog.New(slog.NewTextHandler(logOutput, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := configureCrashOutput(filepath.Join(*dataDir, "logs")); err != nil {
		logger.Warn("configure crash output", "error", err)
	}
	repo, err := storage.Open(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	uds, err := listenUnix(*socketPath)
	if err != nil {
		return err
	}
	defer func() { uds.Close(); _ = os.Remove(*socketPath) }()
	tcp, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		return err
	}
	defer tcp.Close()
	srv := server.New(repo, logger)
	ingestHTTP := &http.Server{Handler: srv.IngestHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	publicHTTP := &http.Server{Handler: srv.PublicHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	retentionJob := storage.NewRetentionJob(repo, storage.RetentionConfig{
		RawEventsDays: *retentionEvents,
		SamplesDays:   *retentionSamples,
		AllDays:       *retentionAll,
	}, logger)
	stopRetention := retentionJob.Start()
	defer stopRetention()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 2)
	go serveWithRetry(ctx, ingestHTTP, uds, func() (net.Listener, error) { return listenUnix(*socketPath) }, "ingest", logger, errCh)
	go serveWithRetry(ctx, publicHTTP, tcp, func() (net.Listener, error) { return net.Listen("tcp", *listenAddr) }, "public", logger, errCh)
	collector := process.NewCollector(repo, *sampleInterval, srv.Insert)
	go collector.Run(ctx)
	logger.Info("OpenClaw Observatory ready", "http", "http://"+*listenAddr, "socket", *socketPath, "database", *dbPath, "version", server.Version,
		"retention_events_days", *retentionEvents, "retention_samples_days", *retentionSamples, "retention_all_days", *retentionAll)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-errCh:
		cancel()
		return err
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = ingestHTTP.Shutdown(shutdownCtx)
	_ = publicHTTP.Shutdown(shutdownCtx)
	return nil
}

func configureCrashOutput(logDir string) error {
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(logDir, "observatoryd-crash.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return debug.SetCrashOutput(file, debug.CrashOptions{})
}

const serverRetryDelay = time.Second

func serveWithRetry(ctx context.Context, srv *http.Server, listener net.Listener, reopen func() (net.Listener, error), name string, logger *slog.Logger, errCh chan<- error) {
	for {
		err := srv.Serve(listener)
		if err == nil || errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
			return
		}
		if !isTemporaryNetError(err) {
			errCh <- fmt.Errorf("%s HTTP server: %w", name, err)
			return
		}
		logger.Warn("temporary HTTP server failure; retrying listener", "server", name, "error", err, "retry_in", serverRetryDelay)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(serverRetryDelay):
			}
			listener, err = reopen()
			if err == nil {
				break
			}
			if !isTemporaryNetError(err) {
				errCh <- fmt.Errorf("reopen %s listener: %w", name, err)
				return
			}
			logger.Warn("temporary listener reopen failure; retrying", "server", name, "error", err)
		}
	}
}

func isTemporaryNetError(err error) bool {
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return true
	}
	var temporary interface{ Temporary() bool }
	return errors.As(err, &temporary) && temporary.Temporary()
}

func listenUnix(path string) (net.Listener, error) {
	if conn, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		conn.Close()
		return nil, fmt.Errorf("another daemon is listening on %s", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}
