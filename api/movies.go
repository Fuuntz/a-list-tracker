package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/fuuntz/a-list-tracker/shared"
)

type RSS struct {
	Channel Channel `xml:"channel"`
}

type Channel struct {
	Items []Item `xml:"item"`
}

type Item struct {
	GUID        string `xml:"guid"`
	Link        string `xml:"link"`
	FilmTitle   string `xml:"https://letterboxd.com filmTitle"`
	FilmYear    string `xml:"https://letterboxd.com filmYear"`
	WatchedDate string `xml:"https://letterboxd.com watchedDate"`
}

type MovieResponse struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	ReleaseYear *int   `json:"releaseYear,omitempty"`
	Link        string `json:"link,omitempty"`
	WatchedDate string `json:"watchedDate"`
	Status      string `json:"status"`
}

var letterboxdClient = &http.Client{Timeout: 10 * time.Second}

func MoviesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	if err := shared.InitDB(); err != nil {
		log.Printf("Database initialization failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database initialization failed"})
		return
	}
	user, _ := shared.UserFromContext(r.Context())

	rows, err := shared.DB.QueryContext(r.Context(), `
SELECT id, title, release_year, letterboxd_uri, watched_on::TEXT, status
FROM viewings
WHERE user_id = $1
ORDER BY watched_on DESC, id DESC`, user.ID)
	if err != nil {
		log.Printf("Failed to load viewings: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load viewing history"})
		return
	}
	defer rows.Close()

	movies := make([]MovieResponse, 0)
	for rows.Next() {
		var movie MovieResponse
		var releaseYear sql.NullInt64
		var link sql.NullString
		if err := rows.Scan(&movie.ID, &movie.Title, &releaseYear, &link, &movie.WatchedDate, &movie.Status); err != nil {
			log.Printf("Failed to read viewing: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load viewing history"})
			return
		}
		if releaseYear.Valid {
			year := int(releaseYear.Int64)
			movie.ReleaseYear = &year
		}
		if link.Valid {
			movie.Link = link.String
		}
		movies = append(movies, movie)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load viewing history"})
		return
	}
	writeJSON(w, http.StatusOK, movies)
}

func SyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	if err := shared.InitDB(); err != nil {
		log.Printf("Database initialization failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database initialization failed"})
		return
	}
	user, _ := shared.UserFromContext(r.Context())

	var username string
	err := shared.DB.QueryRowContext(r.Context(),
		`SELECT letterboxd_username FROM user_settings WHERE user_id = $1`, user.ID,
	).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) || username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Add your Letterboxd username in settings first"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load settings"})
		return
	}

	rssURL := fmt.Sprintf("https://letterboxd.com/%s/rss/", url.PathEscape(username))
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rssURL, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to prepare RSS request"})
		return
	}
	request.Header.Set("User-Agent", "A-List-Tracker/1.0")
	response, err := letterboxdClient.Do(request)
	if err != nil {
		log.Printf("Failed to fetch Letterboxd RSS feed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Failed to fetch Letterboxd RSS feed"})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		log.Printf("Letterboxd RSS feed returned %s", response.Status)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Letterboxd RSS feed returned an error"})
		return
	}

	var feed RSS
	decoder := xml.NewDecoder(http.MaxBytesReader(w, response.Body, 5<<20))
	if err := decoder.Decode(&feed); err != nil {
		log.Printf("Failed to parse Letterboxd RSS feed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Failed to parse Letterboxd RSS feed"})
		return
	}

	tx, err := shared.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start synchronization"})
		return
	}
	defer tx.Rollback()

	processed := 0
	for _, item := range feed.Channel.Items {
		if item.WatchedDate == "" || strings.TrimSpace(item.FilmTitle) == "" {
			continue
		}
		watchedOn, err := time.Parse("2006-01-02", item.WatchedDate)
		if err != nil {
			continue
		}
		var releaseYear *int
		if year, err := strconv.Atoi(item.FilmYear); err == nil && year >= 1800 && year <= 3000 {
			releaseYear = &year
		}
		sourceKey := strings.TrimSpace(item.GUID)
		if sourceKey == "" {
			sourceKey = strings.TrimSpace(item.Link) + "|" + item.WatchedDate
		}
		canonicalKey := canonicalViewingKey(item.FilmTitle, releaseYear, watchedOn)
		metadata, _ := json.Marshal(map[string]string{
			"guid": item.GUID,
			"link": item.Link,
		})

		var viewingID int64
		err = tx.QueryRowContext(r.Context(), `
INSERT INTO viewings (user_id, title, release_year, watched_on, letterboxd_uri, canonical_key)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
ON CONFLICT (user_id, canonical_key) DO UPDATE SET
    title = EXCLUDED.title,
    release_year = COALESCE(EXCLUDED.release_year, viewings.release_year),
    letterboxd_uri = COALESCE(EXCLUDED.letterboxd_uri, viewings.letterboxd_uri),
    updated_at = NOW()
RETURNING id`, user.ID, strings.TrimSpace(item.FilmTitle), releaseYear, watchedOn, strings.TrimSpace(item.Link), canonicalKey).Scan(&viewingID)
		if err != nil {
			returnSyncError(w, err)
			return
		}
		if _, err := tx.ExecContext(r.Context(), `
INSERT INTO viewing_sources (user_id, viewing_id, source_type, source_key, source_metadata)
VALUES ($1, $2, 'rss', $3, $4)
ON CONFLICT (user_id, source_type, source_key) DO UPDATE SET
    viewing_id = EXCLUDED.viewing_id,
    source_metadata = EXCLUDED.source_metadata,
    last_seen_at = NOW()`, user.ID, viewingID, sourceKey, metadata); err != nil {
			returnSyncError(w, err)
			return
		}
		processed++
	}

	if err := tx.Commit(); err != nil {
		returnSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "processed": processed})
}

func returnSyncError(w http.ResponseWriter, err error) {
	log.Printf("Failed to synchronize RSS viewing: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save synchronized viewings"})
}

// canonicalViewingKey intentionally matches Letterboxd's own diary import
// behavior: the same film on the same watched date is one diary event.
func canonicalViewingKey(title string, releaseYear *int, watchedOn time.Time) string {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		if unicode.IsSpace(r) {
			return ' '
		}
		return -1
	}, title)
	normalized = strings.Join(strings.Fields(normalized), " ")
	year := 0
	if releaseYear != nil {
		year = *releaseYear
	}
	value := fmt.Sprintf("%s|%d|%s", normalized, year, watchedOn.Format("2006-01-02"))
	digest := sha256.Sum256([]byte(value))
	return "letterboxd-event:" + hex.EncodeToString(digest[:])
}
