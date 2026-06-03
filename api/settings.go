package api

import (
	"encoding/json"
	"net/http"

	"github.com/fuuntz/a-list-tracker/shared"
)

type Settings struct {
	Username    string  `json:"username"`
	MonthlyCost float64 `json:"monthlyCost"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	if err := shared.InitDB(); err != nil {
		http.Error(w, "Database initialization failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		var s Settings
		err := shared.DB.QueryRow("SELECT username, monthly_cost FROM settings WHERE id = 1").Scan(&s.Username, &s.MonthlyCost)
		if err != nil {
			http.Error(w, "Failed to load settings", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(s)
		return
	}

	if r.Method == http.MethodPost {
		var s Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		_, err := shared.DB.Exec("UPDATE settings SET username = $1, monthly_cost = $2 WHERE id = 1", s.Username, s.MonthlyCost)
		if err != nil {
			http.Error(w, "Failed to update settings", http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
