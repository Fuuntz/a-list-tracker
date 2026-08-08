# A-List Tracker

A private Go web app for tracking which Letterboxd diary entries used AMC
A-List.

It provides separate user accounts, persistent RSS history, Letterboxd export
imports, duplicate detection, editable classifications, membership periods,
and per-account backup and reset.

## Run locally

Requires Go 1.21+ and PostgreSQL. Set a pooled database connection and start the
server:

```sh
export POSTGRES_URL='postgresql://...'
go run .
```

Open <http://localhost:3000>. Database migrations run automatically.

## Deploy

Connect a Neon database to the Vercel project and ensure `POSTGRES_URL` or
`DATABASE_URL` is available in the Production environment. Vercel serves the Go
application from `main.go` without a custom build command.

Create a user's one-time setup link from a trusted terminal:

```sh
go run ./cmd/accounts invite \
  --base-url https://your-project.vercel.app \
  person@example.com
```

The terminal must have `POSTGRES_URL` or `DATABASE_URL` set to the same Neon
database. Other account commands are available with:

```sh
go run ./cmd/accounts
```

## Importing history

In the app, add an AMC membership period, then upload a Letterboxd export ZIP or
`diary.csv` from **Data**. The preview excludes entries outside the membership
period and avoids duplicates from imports or RSS syncs.

## Check the project

```sh
go test ./...
go vet ./...
```
