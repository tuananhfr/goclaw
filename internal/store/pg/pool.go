package pg

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenDB creates a database/sql connection to Postgres using pgx driver.
func OpenDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Sized against GOCLAW_TEKSHOT_JOB_WORKERS: an in-flight job borrows a
	// connection per query, so a larger worker pool needs a larger one here.
	// Defaults keep the original sizing, so an unset environment is unchanged.
	maxOpen := max(envInt("GOCLAW_DB_MAX_OPEN_CONNS", 25), 1)
	maxIdle := max(envInt("GOCLAW_DB_MAX_IDLE_CONNS", 10), 1)
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	slog.Info("postgres connected", "dsn_len", len(dsn), "max_open_conns", maxOpen, "max_idle_conns", maxIdle)
	return db, nil
}

// envInt reads an int from the environment, falling back when unset or unparseable.
func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
