package api

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fuuntz/a-list-tracker/shared"
)

func ExportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	if err := shared.InitDB(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database initialization failed"})
		return
	}
	user, _ := shared.UserFromContext(r.Context())

	settings, err := loadSettings(r, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to export settings"})
		return
	}
	rows, err := shared.DB.QueryContext(r.Context(), `
SELECT watched_on::TEXT, title, release_year, status, letterboxd_uri
FROM viewings
WHERE user_id = $1
ORDER BY watched_on, id`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to export viewing history"})
		return
	}
	defer rows.Close()

	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	viewingsFile, err := archive.Create("viewings.csv")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create export"})
		return
	}
	csvWriter := csv.NewWriter(viewingsFile)
	_ = csvWriter.Write([]string{"Watched Date", "Title", "Year", "Classification", "Letterboxd URI"})
	for rows.Next() {
		var watchedOn, title, status string
		var year sql.NullInt64
		var uri sql.NullString
		if err := rows.Scan(&watchedOn, &title, &year, &status, &uri); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create export"})
			return
		}
		yearValue := ""
		if year.Valid {
			yearValue = strconv.FormatInt(year.Int64, 10)
		}
		_ = csvWriter.Write([]string{
			watchedOn,
			safeSpreadsheetCell(title),
			yearValue,
			status,
			safeSpreadsheetCell(uri.String),
		})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create export"})
		return
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create export"})
		return
	}

	settingsFile, err := archive.Create("settings.json")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create export"})
		return
	}
	encoder := json.NewEncoder(settingsFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create export"})
		return
	}
	if err := archive.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create export"})
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="a-list-tracker-%s.zip"`, time.Now().Format("2006-01-02")))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
}

func ResetDataHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.Confirmation != "DELETE MY TRACKER DATA" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Confirmation phrase does not match"})
		return
	}
	if err := shared.InitDB(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database initialization failed"})
		return
	}
	user, _ := shared.UserFromContext(r.Context())

	tx, err := shared.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to reset tracker data"})
		return
	}
	defer tx.Rollback()
	for _, query := range []string{
		`DELETE FROM import_jobs WHERE user_id = $1`,
		`DELETE FROM viewings WHERE user_id = $1`,
		`DELETE FROM membership_periods WHERE user_id = $1`,
		`DELETE FROM user_settings WHERE user_id = $1`,
	} {
		if _, err := tx.ExecContext(r.Context(), query, user.ID); err != nil {
			log.Printf("Failed to reset tracker data: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to reset tracker data"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to reset tracker data"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func safeSpreadsheetCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
