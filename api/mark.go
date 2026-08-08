package api

import (
	"log"
	"net/http"

	"github.com/fuuntz/a-list-tracker/shared"
)

type MovieMark struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

func MarkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	if err := shared.InitDB(); err != nil {
		log.Printf("Database initialization failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database initialization failed"})
		return
	}

	var mark MovieMark
	if !readJSON(w, r, &mark) {
		return
	}
	if mark.ID <= 0 || !validViewingStatus(mark.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid viewing update"})
		return
	}
	user, _ := shared.UserFromContext(r.Context())
	result, err := shared.DB.ExecContext(r.Context(), `
UPDATE viewings
SET status = $1, updated_at = NOW()
WHERE id = $2 AND user_id = $3`, mark.Status, mark.ID, user.ID)
	if err != nil {
		log.Printf("Failed to update viewing: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update viewing"})
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Viewing not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func validViewingStatus(status string) bool {
	switch status {
	case "unreviewed", "a_list", "not_a_list", "excluded":
		return true
	default:
		return false
	}
}
