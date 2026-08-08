package api

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/fuuntz/a-list-tracker/shared"
)

const maxImportUpload = 4 << 20

type parsedImportRow struct {
	rowNumber     int
	title         string
	releaseYear   *int
	watchedOn     *time.Time
	letterboxdURI string
	canonicalKey  *string
	disposition   string
	reason        string
}

type ImportSample struct {
	Title       string `json:"title"`
	WatchedDate string `json:"watchedDate,omitempty"`
	Disposition string `json:"disposition"`
}

type ImportPreview struct {
	JobID      int64          `json:"jobId"`
	Eligible   int            `json:"eligible"`
	Duplicates int            `json:"duplicates"`
	TooOld     int            `json:"tooOld"`
	Invalid    int            `json:"invalid"`
	Samples    []ImportSample `json:"samples"`
}

func ImportPreviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	if err := shared.InitDB(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database initialization failed"})
		return
	}
	user, _ := shared.UserFromContext(r.Context())
	// Abandoned previews contain parsed rows, so do not retain them indefinitely.
	if _, err := shared.DB.ExecContext(r.Context(), `
DELETE FROM import_jobs
WHERE user_id = $1
  AND status = 'pending'
  AND created_at < NOW() - INTERVAL '24 hours'`, user.ID); err != nil {
		log.Printf("Failed to remove expired import previews: %v", err)
	}

	var cutoffValue sql.NullTime
	err := shared.DB.QueryRowContext(r.Context(),
		`SELECT MIN(starts_on) FROM membership_periods WHERE user_id = $1`, user.ID,
	).Scan(&cutoffValue)
	if err == nil && !cutoffValue.Valid {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Add an A-List membership period before importing"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load membership settings"})
		return
	}
	cutoff := cutoffValue.Time

	filename, contents, err := readImportUpload(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rows, err := parseLetterboxdExport(filename, contents)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	existing, err := existingCanonicalKeys(r, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to compare import with existing history"})
		return
	}
	seen := make(map[string]bool, len(rows))
	preview := ImportPreview{Samples: make([]ImportSample, 0, 8)}
	for index := range rows {
		row := &rows[index]
		if row.disposition == "invalid" {
			preview.Invalid++
		} else if row.watchedOn.Before(cutoff) {
			row.disposition = "too_old"
			row.reason = "Before the first A-List membership period"
			preview.TooOld++
		} else if existing[*row.canonicalKey] || seen[*row.canonicalKey] {
			row.disposition = "duplicate"
			row.reason = "Already saved or repeated in this file"
			preview.Duplicates++
		} else {
			row.disposition = "eligible"
			seen[*row.canonicalKey] = true
			preview.Eligible++
		}
		if len(preview.Samples) < 8 {
			sample := ImportSample{Title: row.title, Disposition: row.disposition}
			if row.watchedOn != nil {
				sample.WatchedDate = row.watchedOn.Format("2006-01-02")
			}
			preview.Samples = append(preview.Samples, sample)
		}
	}

	tx, err := shared.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create import preview"})
		return
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(r.Context(), `
INSERT INTO import_jobs (
    user_id, filename, eligible_count, duplicate_count, too_old_count, invalid_count
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id`, user.ID, filename, preview.Eligible, preview.Duplicates, preview.TooOld, preview.Invalid).Scan(&preview.JobID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create import preview"})
		return
	}
	for _, row := range rows {
		if _, err := tx.ExecContext(r.Context(), `
INSERT INTO import_rows (
    job_id, row_number, title, release_year, watched_on, letterboxd_uri,
    canonical_key, disposition, reason
) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, NULLIF($9, ''))`,
			preview.JobID, row.rowNumber, row.title, row.releaseYear, row.watchedOn,
			row.letterboxdURI, row.canonicalKey, row.disposition, row.reason,
		); err != nil {
			log.Printf("Failed to save import preview row: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create import preview"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create import preview"})
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func ImportConfirmHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var input struct {
		JobID int64 `json:"jobId"`
	}
	if !readJSON(w, r, &input) || input.JobID <= 0 {
		return
	}
	if err := shared.InitDB(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database initialization failed"})
		return
	}
	user, _ := shared.UserFromContext(r.Context())

	tx, err := shared.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start import"})
		return
	}
	defer tx.Rollback()

	var status string
	err = tx.QueryRowContext(r.Context(), `
SELECT status
FROM import_jobs
WHERE id = $1 AND user_id = $2 AND created_at > NOW() - INTERVAL '24 hours'
FOR UPDATE`, input.JobID, user.ID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) || status != "pending" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "This import preview is invalid, expired, or already completed"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load import preview"})
		return
	}

	rows, err := tx.QueryContext(r.Context(), `
SELECT row_number, title, release_year, watched_on, letterboxd_uri, canonical_key
FROM import_rows
WHERE job_id = $1 AND disposition = 'eligible'
ORDER BY row_number`, input.JobID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load import rows"})
		return
	}
	type eligibleRow struct {
		rowNumber   int
		title       string
		releaseYear sql.NullInt64
		watchedOn   time.Time
		uri         sql.NullString
		canonical   string
	}
	eligible := make([]eligibleRow, 0)
	for rows.Next() {
		var row eligibleRow
		if err := rows.Scan(&row.rowNumber, &row.title, &row.releaseYear, &row.watchedOn, &row.uri, &row.canonical); err != nil {
			rows.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load import rows"})
			return
		}
		eligible = append(eligible, row)
	}
	if err := rows.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load import rows"})
		return
	}

	imported := 0
	for _, row := range eligible {
		var viewingID int64
		err := tx.QueryRowContext(r.Context(), `
INSERT INTO viewings (user_id, title, release_year, watched_on, letterboxd_uri, canonical_key)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, canonical_key) DO UPDATE SET
    title = EXCLUDED.title,
    release_year = COALESCE(EXCLUDED.release_year, viewings.release_year),
    letterboxd_uri = COALESCE(EXCLUDED.letterboxd_uri, viewings.letterboxd_uri),
    updated_at = NOW()
RETURNING id`, user.ID, row.title, row.releaseYear, row.watchedOn, row.uri, row.canonical).Scan(&viewingID)
		if err != nil {
			log.Printf("Failed to import viewing: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to import viewing history"})
			return
		}
		sourceKey := row.canonical
		if row.uri.Valid {
			sourceKey = row.uri.String + "|" + row.watchedOn.Format("2006-01-02")
		}
		if _, err := tx.ExecContext(r.Context(), `
INSERT INTO viewing_sources (user_id, viewing_id, source_type, source_key, source_metadata)
VALUES ($1, $2, 'letterboxd_import', $3, jsonb_build_object('row', $4::INTEGER))
ON CONFLICT (user_id, source_type, source_key) DO UPDATE SET
    viewing_id = EXCLUDED.viewing_id,
    last_seen_at = NOW()`, user.ID, viewingID, sourceKey, row.rowNumber); err != nil {
			log.Printf("Failed to save import source: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to import viewing history"})
			return
		}
		imported++
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE import_jobs SET status = 'completed', completed_at = NOW() WHERE id = $1`, input.JobID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to complete import"})
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM import_rows WHERE job_id = $1`, input.JobID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to complete import"})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to complete import"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "imported": imported})
}

func readImportUpload(w http.ResponseWriter, r *http.Request) (string, []byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportUpload)
	if err := r.ParseMultipartForm(maxImportUpload); err != nil {
		return "", nil, errors.New("upload a Letterboxd ZIP or diary CSV smaller than 4 MB")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", nil, errors.New("choose a Letterboxd export file")
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxImportUpload+1))
	if err != nil || len(contents) > maxImportUpload {
		return "", nil, errors.New("upload a Letterboxd ZIP or diary CSV smaller than 4 MB")
	}
	return filepath.Base(header.Filename), contents, nil
}

func parseLetterboxdExport(filename string, contents []byte) ([]parsedImportRow, error) {
	csvContents := contents
	if strings.EqualFold(filepath.Ext(filename), ".zip") || bytes.HasPrefix(contents, []byte("PK\x03\x04")) {
		reader, err := zip.NewReader(bytes.NewReader(contents), int64(len(contents)))
		if err != nil {
			return nil, errors.New("the uploaded ZIP file could not be read")
		}
		csvContents = nil
		for _, file := range reader.File {
			if strings.EqualFold(filepath.Base(file.Name), "diary.csv") {
				opened, err := file.Open()
				if err != nil {
					return nil, errors.New("diary.csv could not be opened")
				}
				csvContents, err = io.ReadAll(io.LimitReader(opened, maxImportUpload+1))
				opened.Close()
				if err != nil || len(csvContents) > maxImportUpload {
					return nil, errors.New("diary.csv is too large to import")
				}
				break
			}
		}
		if csvContents == nil {
			return nil, errors.New("the ZIP file does not contain diary.csv")
		}
	} else if !strings.EqualFold(filepath.Ext(filename), ".csv") {
		return nil, errors.New("upload the Letterboxd export ZIP or diary.csv")
	}

	reader := csv.NewReader(bytes.NewReader(csvContents))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, errors.New("diary.csv is empty or invalid")
	}
	columns := make(map[string]int, len(header))
	for index, value := range header {
		columns[normalizeCSVHeader(value)] = index
	}
	titleColumn, titleOK := firstColumn(columns, "name", "title")
	dateColumn, dateOK := firstColumn(columns, "watcheddate")
	if !titleOK || !dateOK {
		return nil, errors.New("the CSV must contain Name (or Title) and Watched Date columns")
	}
	yearColumn, hasYear := firstColumn(columns, "year")
	uriColumn, hasURI := firstColumn(columns, "letterboxduri", "url")

	result := make([]parsedImportRow, 0)
	for rowNumber := 2; ; rowNumber++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		row := parsedImportRow{rowNumber: rowNumber, disposition: "invalid"}
		if err != nil {
			row.title = fmt.Sprintf("Row %d", rowNumber)
			row.reason = "Invalid CSV row"
			result = append(result, row)
			continue
		}
		row.title = strings.TrimSpace(csvValue(record, titleColumn))
		if row.title == "" {
			row.title = fmt.Sprintf("Row %d", rowNumber)
			row.reason = "Missing movie title"
			result = append(result, row)
			continue
		}
		watchedOn, err := time.Parse("2006-01-02", strings.TrimSpace(csvValue(record, dateColumn)))
		if err != nil {
			row.reason = "Missing or invalid watched date"
			result = append(result, row)
			continue
		}
		row.watchedOn = &watchedOn
		if hasYear {
			if year, err := strconv.Atoi(strings.TrimSpace(csvValue(record, yearColumn))); err == nil && year >= 1800 && year <= 3000 {
				row.releaseYear = &year
			}
		}
		if hasURI {
			row.letterboxdURI = strings.TrimSpace(csvValue(record, uriColumn))
		}
		canonical := canonicalViewingKey(row.title, row.releaseYear, watchedOn)
		row.canonicalKey = &canonical
		row.disposition = "eligible"
		result = append(result, row)
	}
	if len(result) == 0 {
		return nil, errors.New("diary.csv contains no viewing rows")
	}
	return result, nil
}

func existingCanonicalKeys(r *http.Request, userID int64) (map[string]bool, error) {
	rows, err := shared.DB.QueryContext(r.Context(),
		`SELECT canonical_key FROM viewings WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		result[key] = true
	}
	return result, rows.Err()
}

func normalizeCSVHeader(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimPrefix(value, "\ufeff"))
}

func firstColumn(columns map[string]int, names ...string) (int, bool) {
	for _, name := range names {
		if index, ok := columns[name]; ok {
			return index, true
		}
	}
	return 0, false
}

func csvValue(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return record[index]
}
