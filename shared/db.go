package shared

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	DB   *sql.DB
	dbMu sync.Mutex
)

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := createSchema(ctx, db); err != nil {
		db.Close()
		return fmt.Errorf("initialize database schema: %w", err)
	}

	DB = db
	return nil
}

func createSchema(ctx context.Context, db *sql.DB) error {
	settingsQuery := `
	CREATE TABLE IF NOT EXISTS settings (
		id INT PRIMARY KEY,
		username VARCHAR(255) NOT NULL,
		monthly_cost DECIMAL(10, 2) NOT NULL
	);
	`
	_, err := db.ExecContext(ctx, settingsQuery)
	if err != nil {
		log.Printf("Error creating settings table: %v", err)
		return err
	}

	// Insert default settings if they don't exist
	defaultSettings := `
	INSERT INTO settings (id, username, monthly_cost)
	VALUES (1, '', 30.00)
	ON CONFLICT (id) DO NOTHING;
	`
	_, err = db.ExecContext(ctx, defaultSettings)
	if err != nil {
		log.Printf("Error inserting default settings: %v", err)
		return err
	}

	marksQuery := `
	CREATE TABLE IF NOT EXISTS movie_marks (
		letterboxd_id VARCHAR(255) PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		watched_date DATE,
		is_a_list BOOLEAN NOT NULL
	);
	`
	_, err = db.ExecContext(ctx, marksQuery)
	if err != nil {
		log.Printf("Error creating movie_marks table: %v", err)
		return err
	}

	return nil
}
