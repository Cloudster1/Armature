// Command migrate applies the embedded goose migrations to the primary.
//
// Usage:
//
//	migrate up            apply all pending migrations
//	migrate down          roll back the most recent migration
//	migrate status        show which migrations have been applied
//	migrate version       print the current schema version
//	migrate up-to <n>     migrate up to and including version n
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/armature/armature/backend/internal/config"
	"github.com/armature/armature/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"up"}
	}
	command := args[0]

	// Migrations always run against the primary, never a replica.
	pool, err := waitForPrimary(ctx, cfg.DB.PrimaryURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	switch command {
	case "up":
		if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
			return err
		}
	case "up-to":
		if len(args) < 2 {
			return errors.New("up-to requires a version")
		}
		v, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid version %q: %w", args[1], err)
		}
		if err := goose.UpToContext(ctx, sqlDB, ".", v); err != nil {
			return err
		}
	case "down":
		if err := goose.DownContext(ctx, sqlDB, "."); err != nil {
			return err
		}
	case "status":
		goose.SetLogger(log{})
		return goose.StatusContext(ctx, sqlDB, ".")
	case "version":
		v, err := goose.GetDBVersionContext(ctx, sqlDB)
		if err != nil {
			return err
		}
		fmt.Println(v)
		return nil
	default:
		return fmt.Errorf("unknown command %q", command)
	}

	v, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return err
	}
	slog.Info("migrations applied", "command", command, "schema_version", v)
	return nil
}

// waitForPrimary retries the initial connection so that the migrate container
// can start alongside Postgres rather than strictly after it.
func waitForPrimary(ctx context.Context, url string) (*pgxpool.Pool, error) {
	const attempts = 30
	var lastErr error
	for i := range attempts {
		pool, err := pgxpool.New(ctx, url)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err
		slog.Info("waiting for database", "attempt", i+1, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("database unreachable after %d attempts: %w", attempts, lastErr)
}

// log adapts goose's logger interface to stdout for the status command.
type log struct{}

func (log) Fatalf(format string, v ...any) { fmt.Printf(format, v...); os.Exit(1) }
func (log) Printf(format string, v ...any) { fmt.Printf(format, v...) }
