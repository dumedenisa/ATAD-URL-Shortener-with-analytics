// Entry point for your project.
//
// HTTP routing lives in internal/httpapi; this package owns process startup,
// version output, and graceful shutdown.
//
// --version must also keep working. It prints one line of JSON and exits 0.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"atad-project/internal/httpapi"
	"atad-project/internal/storage"
	"atad-project/internal/version"
)

// started is the moment the process came up; /health reports the difference.
var started = time.Now()

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" {
			return version.WriteJSON(os.Stdout)
		}
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	databasePath := os.Getenv("DATABASE_PATH")
	if databasePath == "" {
		databasePath = "data/links.db"
	}
	store, err := storage.Open(databasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      httpapi.NewHandler(started, store),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Shut down cleanly on Ctrl-C or on the signal Docker sends when stopping
	// the container, so in-flight requests are allowed to finish.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "commit", version.Current().Commit)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
