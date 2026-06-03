package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/fuuntz/a-list-tracker/shared"
)

type MovieMark struct {
	LetterboxdID string    `json:"id"`
	Title        string    `json:"title"`
	WatchedDate  time.Time `json:"watchedDate"`
	IsAList      bool      `json:"isAList"`
}

func MarkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := shared.InitDB(); err != nil {
		http.Error(w, "Database initialization failed", http.StatusInternalServerError)
		return
	}

	var m MovieMark
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	query := `
	INSERT INTO movie_marks (letterboxd_id, title, watched_date, is_a_list)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (letterboxd_id) DO UPDATE 
	SET is_a_list = EXCLUDED.is_a_list
	`

	_, err := shared.DB.Exec(query, m.LetterboxdID, m.Title, m.WatchedDate, m.IsAList)
	if err != nil {
		http.Error(w, "Failed to save mark", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
