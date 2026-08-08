package shared

import (
	"context"
	"database/sql"
	"fmt"
)

type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{
		version: 1,
		sql: `
CREATE TABLE users (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (email = LOWER(email))
);

CREATE TABLE account_tokens (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    purpose TEXT NOT NULL CHECK (purpose IN ('initial_setup', 'password_reset')),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX account_tokens_user_idx ON account_tokens(user_id, purpose);

CREATE TABLE sessions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expiry_idx ON sessions(expires_at);

CREATE TABLE login_throttles (
    identifier_hash BYTEA PRIMARY KEY,
    failure_count INTEGER NOT NULL,
    last_failed_at TIMESTAMPTZ NOT NULL,
    blocked_until TIMESTAMPTZ
);

CREATE TABLE user_settings (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    letterboxd_username TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE membership_periods (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    starts_on DATE NOT NULL,
    ends_on DATE,
    monthly_cost_cents INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (monthly_cost_cents >= 0),
    CHECK (ends_on IS NULL OR ends_on >= starts_on),
    UNIQUE (user_id, starts_on)
);
CREATE INDEX membership_periods_user_idx ON membership_periods(user_id, starts_on);

CREATE TABLE viewings (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    release_year INTEGER,
    watched_on DATE NOT NULL,
    letterboxd_uri TEXT,
    canonical_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unreviewed'
        CHECK (status IN ('unreviewed', 'a_list', 'not_a_list', 'excluded')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, canonical_key),
    UNIQUE (user_id, id)
);
CREATE INDEX viewings_user_date_idx ON viewings(user_id, watched_on DESC);
CREATE INDEX viewings_user_status_idx ON viewings(user_id, status);

CREATE TABLE viewing_sources (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    viewing_id BIGINT NOT NULL,
    source_type TEXT NOT NULL CHECK (source_type IN ('rss', 'letterboxd_import', 'manual')),
    source_key TEXT NOT NULL,
    source_metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (user_id, viewing_id) REFERENCES viewings(user_id, id) ON DELETE CASCADE,
    UNIQUE (user_id, source_type, source_key)
);
CREATE INDEX viewing_sources_viewing_idx ON viewing_sources(viewing_id);
`,
	},
	{
		version: 2,
		sql: `
CREATE TABLE import_jobs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'cancelled')),
    eligible_count INTEGER NOT NULL DEFAULT 0,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    too_old_count INTEGER NOT NULL DEFAULT 0,
    invalid_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX import_jobs_user_idx ON import_jobs(user_id, created_at DESC);

CREATE TABLE import_rows (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id BIGINT NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    row_number INTEGER NOT NULL,
    title TEXT NOT NULL,
    release_year INTEGER,
    watched_on DATE,
    letterboxd_uri TEXT,
    canonical_key TEXT,
    disposition TEXT NOT NULL CHECK (disposition IN ('eligible', 'duplicate', 'too_old', 'invalid')),
    reason TEXT,
    UNIQUE (job_id, row_number)
);
CREATE INDEX import_rows_job_disposition_idx ON import_rows(job_id, disposition);
`,
	},
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Serialize migrations across concurrent Vercel function starts.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(736482901)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return err
	}

	for _, item := range migrations {
		var applied bool
		err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`,
			item.version,
		).Scan(&applied)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		if _, err := tx.ExecContext(ctx, item.sql); err != nil {
			return fmt.Errorf("migration %d: %w", item.version, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, item.version); err != nil {
			return err
		}
	}

	return tx.Commit()
}
