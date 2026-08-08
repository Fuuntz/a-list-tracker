# A-List Tracker

A private Go web app for tracking which Letterboxd diary viewings used AMC
A-List. Each account has separate settings and history. The app stores durable
viewing records in PostgreSQL, so it is not limited to the entries currently
available in Letterboxd's RSS feed.

## Features

- Closed registration with locally generated, one-time setup links
- Argon2id password hashing and revocable database-backed sessions
- Completely separate data for each account
- Persistent Letterboxd RSS synchronization
- Letterboxd export preview and import
- Cross-source deduplication by film, release year, and watched date
- Searchable history with editable A-List classifications
- Multiple membership periods for pauses and price changes
- Per-account ZIP export and confirmed data reset

The app has no public sign-up endpoint and does not use an external identity
provider or email delivery service.

## Requirements

- Go 1.21 or newer
- PostgreSQL (the deployed project uses Neon)

Set either `POSTGRES_URL` or `DATABASE_URL` to the pooled PostgreSQL connection
string:

```sh
export POSTGRES_URL='postgresql://user:password@host/database?sslmode=require'
go run .
```

Open <http://localhost:3000>. Database migrations are applied automatically and
are serialized with a PostgreSQL advisory lock.

## Creating accounts

Accounts are created from a trusted terminal, not from the public website. With
the database connection variable still set, generate the first setup link:

```sh
go run ./cmd/accounts invite --base-url http://localhost:3000 you@example.com
```

Open the printed URL and choose a password of at least 12 characters. The link
expires after 24 hours and works once. Generate the second person's link the
same way.

Other account commands:

```sh
go run ./cmd/accounts list
go run ./cmd/accounts reset-password --base-url http://localhost:3000 you@example.com
go run ./cmd/accounts disable you@example.com
go run ./cmd/accounts enable you@example.com
```

The reset command prints a one-time password-reset URL; it does not send email.

## Deploying to Vercel

Vercel detects `main.go` as a standalone Go server. No custom build command or
rewrite is required.

1. Connect the Neon database to the Vercel project.
2. Confirm that `POSTGRES_URL` or `DATABASE_URL` is present under **Settings →
   Environment Variables** for Production.
3. Deploy this repository.
4. Copy the same pooled connection string from Neon for one local terminal
   session. Do not commit it or paste it into a tracked file.
5. Generate an account link using the deployed address:

```sh
export POSTGRES_URL='your pooled Neon connection string'
go run ./cmd/accounts invite --base-url https://your-project.vercel.app you@example.com
```

6. Open the printed link, finish setup, and configure the Letterboxd username
   and membership period.

The first database-backed request applies the new migrations. The legacy
prototype tables (`settings` and `movie_marks`) are deliberately left alone and
are ignored by the new application. They can be dropped later after the new
deployment has been verified.

## Historical import

Download a Letterboxd data export, then upload either the ZIP or its `diary.csv`
file from the app's **Data** page. The app first shows a preview:

- Eligible diary entries
- Entries already stored
- Entries before the first A-List membership period
- Invalid rows

Only eligible rows are saved after confirmation. Import previews expire after
24 hours, and parsed preview rows are deleted when an import completes.

Letterboxd treats the same film on the same watched date as one diary entry. The
tracker follows that behavior when reconciling RSS and imported records.

## Data reset and backup

Each user can download a ZIP containing `viewings.csv` and `settings.json`.
The **Reset tracker data** action deletes only that signed-in user's viewing,
import, Letterboxd, and membership data. It keeps the login account. A later RSS
sync or import can add viewings again.

## Verification

```sh
go test ./...
go vet ./...
```
