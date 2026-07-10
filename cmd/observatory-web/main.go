package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/webserver"
)

var buildID = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("observatory-web stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	listenAddr := flag.String("listen", "127.0.0.1:10086", "frontend listen address")
	backendURL := flag.String("backend", "http://127.0.0.1:10087", "Observatory API base URL")
	webRoot := flag.String("web-root", filepath.Join(home, ".local", "share", "openclaw-observatory", "web", "current"), "built frontend directory")
	flag.Parse()

	handler, err := webserver.New(webserver.Config{Root: *webRoot, BackendURL: *backendURL, BuildID: buildID})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("OpenClaw Observatory web ready", "http", "http://"+*listenAddr, "backend", *backendURL, "root", *webRoot, "buildId", buildID)
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
	case err := <-errCh:
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
