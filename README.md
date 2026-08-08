# A-List Tracker

A small Go web app that uses a Letterboxd RSS feed to track which watched
movies were seen with AMC A-List. It stores settings and movie classifications
in PostgreSQL and calculates the effective average ticket price from the
monthly subscription cost.

## Local development

Requirements:

- Go 1.21 or newer
- A PostgreSQL database

Set either `POSTGRES_URL` or `DATABASE_URL` to a PostgreSQL connection string,
then start the app:

```sh
export POSTGRES_URL='postgresql://user:password@host:5432/database?sslmode=require'
go run .
```

Open <http://localhost:3000>. The app creates its `settings` and `movie_marks`
tables on the first API request.

## Deploying to Vercel

Vercel automatically detects `main.go` as a standalone Go server. No custom
build command or rewrite is required.

1. Create or connect a PostgreSQL database.
2. In the Vercel project, add `POSTGRES_URL` or `DATABASE_URL` under
   **Settings > Environment Variables**. Enable it for Production and Preview
   if both environments should work.
3. Push this repository and redeploy.

The app currently has no authentication and uses one shared settings record,
so a public deployment should be treated as a single-user app.
