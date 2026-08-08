package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fuuntz/a-list-tracker/shared"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	switch os.Args[1] {
	case "invite":
		createLink(ctx, "initial_setup", os.Args[2:])
	case "reset-password":
		createLink(ctx, "password_reset", os.Args[2:])
	case "list":
		listAccounts(ctx)
	case "disable":
		setDisabled(ctx, os.Args[2:], true)
	case "enable":
		setDisabled(ctx, os.Args[2:], false)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Account administration

Usage:
  go run ./cmd/accounts invite [--base-url URL] EMAIL
  go run ./cmd/accounts reset-password [--base-url URL] EMAIL
  go run ./cmd/accounts list
  go run ./cmd/accounts disable EMAIL
  go run ./cmd/accounts enable EMAIL

POSTGRES_URL or DATABASE_URL must point to the application's Neon database.`)
	os.Exit(2)
}

func createLink(ctx context.Context, purpose string, args []string) {
	command := flag.NewFlagSet(purpose, flag.ExitOnError)
	defaultURL := strings.TrimRight(os.Getenv("APP_URL"), "/")
	baseURL := command.String("base-url", defaultURL, "deployed application URL")
	_ = command.Parse(args)
	if command.NArg() != 1 || *baseURL == "" {
		fmt.Fprintln(os.Stderr, "An email and --base-url (or APP_URL) are required.")
		os.Exit(2)
	}

	token, err := shared.CreateAccountToken(ctx, command.Arg(0), purpose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create account link: %v\n", err)
		os.Exit(1)
	}
	// Keep the bearer token in the URL fragment. Browsers do not send fragments
	// to Vercel, so it does not appear in normal HTTP request logs.
	fmt.Printf("%s/#setup=%s\n", strings.TrimRight(*baseURL, "/"), token)
}

func listAccounts(ctx context.Context) {
	mustInitDB()
	rows, err := shared.DB.QueryContext(ctx, `
SELECT email, password_hash IS NOT NULL, disabled, created_at
FROM users
ORDER BY created_at`)
	if err != nil {
		fatal(err)
	}
	defer rows.Close()

	fmt.Println("EMAIL\tSET UP\tDISABLED\tCREATED")
	for rows.Next() {
		var email string
		var setUp, disabled bool
		var created time.Time
		if err := rows.Scan(&email, &setUp, &disabled, &created); err != nil {
			fatal(err)
		}
		fmt.Printf("%s\t%t\t%t\t%s\n", email, setUp, disabled, created.Format(time.RFC3339))
	}
	if err := rows.Err(); err != nil {
		fatal(err)
	}
}

func setDisabled(ctx context.Context, args []string, disabled bool) {
	if len(args) != 1 {
		usage()
	}
	email, err := shared.NormalizeEmail(args[0])
	if err != nil {
		fatal(err)
	}
	mustInitDB()

	tx, err := shared.DB.BeginTx(ctx, nil)
	if err != nil {
		fatal(err)
	}
	defer tx.Rollback()
	var userID int64
	err = tx.QueryRowContext(ctx,
		`UPDATE users SET disabled = $1, updated_at = NOW() WHERE email = $2 RETURNING id`,
		disabled, email,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		fatal(shared.ErrNoAccount)
	}
	if err != nil {
		fatal(err)
	}
	if disabled {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
			fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		fatal(err)
	}
	fmt.Printf("Account %s: %s\n", email, map[bool]string{true: "disabled", false: "enabled"}[disabled])
}

func mustInitDB() {
	if err := shared.InitDB(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
