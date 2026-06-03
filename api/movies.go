package api

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"time"

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
	WatchedDate string `xml:"https://letterboxd.com watchedDate"`
}

type MovieResponse struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	WatchedDate time.Time `json:"watchedDate"`
	Status      string    `json:"status"` // "A-List", "Not A-List", "Unmarked"
}

func MoviesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := shared.InitDB(); err != nil {
		http.Error(w, "Database initialization failed", http.StatusInternalServerError)
		return
	}

	var username string
	err := shared.DB.QueryRow("SELECT username FROM settings WHERE id = 1").Scan(&username)
	if err != nil || username == "" {
		// Return empty list if no username configured
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]MovieResponse{})
		return
	}

	rssURL := fmt.Sprintf("https://letterboxd.com/%s/rss/", username)
	resp, err := http.Get(rssURL)
	if err != nil {
		http.Error(w, "Failed to fetch RSS feed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var feed RSS
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		http.Error(w, "Failed to parse RSS feed", http.StatusInternalServerError)
		return
	}

	// Fetch marks from DB
	rows, err := shared.DB.Query("SELECT letterboxd_id, is_a_list FROM movie_marks")
	if err != nil {
		http.Error(w, "Failed to query database", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	marks := make(map[string]bool)
	for rows.Next() {
		var id string
		var isAList bool
		if err := rows.Scan(&id, &isAList); err == nil {
			marks[id] = isAList
		}
	}

	// Merge
	var result []MovieResponse
	for _, item := range feed.Channel.Items {
		// Skip items that don't have a watched date (e.g. lists, reviews of unseen movies)
		if item.WatchedDate == "" {
			continue
		}

		parsedDate, err := time.Parse("2006-01-02", item.WatchedDate)
		if err != nil {
			continue
		}

		status := "Unmarked"
		if isAList, exists := marks[item.GUID]; exists {
			if isAList {
				status = "A-List"
			} else {
				status = "Not A-List"
			}
		}

		result = append(result, MovieResponse{
			ID:          item.GUID,
			Title:       item.FilmTitle,
			Link:        item.Link,
			WatchedDate: parsedDate,
			Status:      status,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
