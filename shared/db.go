package shared

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	DB   *sql.DB
	dbMu sync.Mutex
)

// InitDB opens the shared database pool and applies non-destructive schema
// migrations. Legacy prototype tables are intentionally left untouched.
func InitDB() error {
	dbMu.Lock()
	defer dbMu.Unlock()

	if DB != nil {
		return nil
	}

	connStr := os.Getenv("POSTGRES_URL")
	if connStr == "" {
		connStr = os.Getenv("DATABASE_URL")
	}
	if connStr == "" {
		return fmt.Errorf("POSTGRES_URL or DATABASE_URL environment variable is not set")
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("connect to database: %w", err)
	}
	if err := applyMigrations(ctx, db); err != nil {
		db.Close()
		return fmt.Errorf("apply database migrations: %w", err)
	}

	DB = db
	return nil
}
