package api

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fuuntz/a-list-tracker/shared"
)

var letterboxdUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`)

type MembershipPeriod struct {
	ID          int64   `json:"id,omitempty"`
	StartsOn    string  `json:"startsOn"`
	EndsOn      *string `json:"endsOn,omitempty"`
	MonthlyCost float64 `json:"monthlyCost"`
}

type Settings struct {
	Username          string             `json:"username"`
	MembershipPeriods []MembershipPeriod `json:"membershipPeriods"`
}

func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	if err := shared.InitDB(); err != nil {
		log.Printf("Database initialization failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database initialization failed"})
		return
	}
	user, _ := shared.UserFromContext(r.Context())

	switch r.Method {
	case http.MethodGet:
		settings, err := loadSettings(r, user.ID)
		if err != nil {
			log.Printf("Failed to load settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load settings"})
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPost:
		var settings Settings
		if !readJSON(w, r, &settings) {
			return
		}
		settings.Username = strings.TrimSpace(strings.TrimPrefix(settings.Username, "@"))
		if settings.Username != "" && !letterboxdUsernamePattern.MatchString(settings.Username) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Enter a valid Letterboxd username"})
			return
		}
		periods, err := validatePeriods(settings.MembershipPeriods)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := saveSettings(r, user.ID, settings.Username, periods); err != nil {
			log.Printf("Failed to update settings: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update settings"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
}

func loadSettings(r *http.Request, userID int64) (Settings, error) {
	settings := Settings{MembershipPeriods: []MembershipPeriod{}}
	err := shared.DB.QueryRowContext(r.Context(),
		`SELECT letterboxd_username FROM user_settings WHERE user_id = $1`, userID,
	).Scan(&settings.Username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return settings, err
	}

	rows, err := shared.DB.QueryContext(r.Context(), `
SELECT id, starts_on::TEXT, ends_on::TEXT, monthly_cost_cents
FROM membership_periods
WHERE user_id = $1
ORDER BY starts_on`, userID)
	if err != nil {
		return settings, err
	}
	defer rows.Close()

	for rows.Next() {
		var period MembershipPeriod
		var endsOn sql.NullString
		var costCents int
		if err := rows.Scan(&period.ID, &period.StartsOn, &endsOn, &costCents); err != nil {
			return settings, err
		}
		if endsOn.Valid {
			period.EndsOn = &endsOn.String
		}
		period.MonthlyCost = float64(costCents) / 100
		settings.MembershipPeriods = append(settings.MembershipPeriods, period)
	}
	return settings, rows.Err()
}

type validatedPeriod struct {
	startsOn    time.Time
	endsOn      *time.Time
	costInCents int
}

func validatePeriods(periods []MembershipPeriod) ([]validatedPeriod, error) {
	if len(periods) > 100 {
		return nil, errors.New("too many membership periods")
	}
	result := make([]validatedPeriod, 0, len(periods))
	for _, period := range periods {
		start, err := time.Parse("2006-01-02", period.StartsOn)
		if err != nil {
			return nil, errors.New("enter a valid membership start date")
		}
		if period.MonthlyCost < 0 || period.MonthlyCost > 10000 {
			return nil, errors.New("enter a valid monthly cost")
		}
		validated := validatedPeriod{
			startsOn:    start,
			costInCents: int(period.MonthlyCost*100 + 0.5),
		}
		if period.EndsOn != nil && *period.EndsOn != "" {
			end, err := time.Parse("2006-01-02", *period.EndsOn)
			if err != nil || end.Before(start) {
				return nil, errors.New("enter a valid membership end date")
			}
			validated.endsOn = &end
		}
		result = append(result, validated)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].startsOn.Before(result[j].startsOn)
	})
	for index := 1; index < len(result); index++ {
		previous := result[index-1]
		if previous.endsOn == nil || !result[index].startsOn.After(*previous.endsOn) {
			return nil, errors.New("membership periods cannot overlap")
		}
	}
	return result, nil
}

func saveSettings(r *http.Request, userID int64, username string, periods []validatedPeriod) error {
	tx, err := shared.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(), `
INSERT INTO user_settings (user_id, letterboxd_username)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET
    letterboxd_username = EXCLUDED.letterboxd_username,
    updated_at = NOW()`, userID, username); err != nil {
		return err
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM membership_periods WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, period := range periods {
		if _, err := tx.ExecContext(r.Context(), `
INSERT INTO membership_periods (user_id, starts_on, ends_on, monthly_cost_cents)
VALUES ($1, $2, $3, $4)`, userID, period.startsOn, period.endsOn, period.costInCents); err != nil {
			return err
		}
	}
	return tx.Commit()
}
