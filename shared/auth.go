package shared

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	SessionCookieName = "alist_session"
	sessionLifetime   = 30 * 24 * time.Hour
	accountTokenLife  = 24 * time.Hour

	argonMemory      = 19 * 1024
	argonIterations  = 2
	argonParallelism = 1
	argonSaltLength  = 16
	argonKeyLength   = 32
)

var (
	ErrInvalidToken  = errors.New("invalid or expired account token")
	ErrAccountExists = errors.New("account already has a password")
	ErrNoAccount     = errors.New("account does not exist")
)

type User struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

type userContextKey struct{}

func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey{}).(User)
	return user, ok
}

func NormalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) > 254 {
		return "", errors.New("enter a valid email address")
	}
	return value, nil
}

func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < 12 {
		return errors.New("password must be at least 12 characters")
	}
	if len(password) > 256 {
		return errors.New("password must be no more than 256 bytes")
	}
	return nil
}

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return false
	}

	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false
	}
	// Bound parameters before allocating memory from a database-provided hash.
	if memory < 7*1024 || memory > 64*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 8 {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false
	}

	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func consumePasswordWork(password string) {
	salt := []byte("a-list-dummy-salt")
	actual := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	_ = subtle.ConstantTimeCompare(actual, make([]byte, argonKeyLength))
}

func randomToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	return token, digest[:], nil
}

func tokenDigest(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

// CreateAccountToken creates an account if needed and returns a one-time setup
// or password-reset token. The raw token is never stored in the database.
func CreateAccountToken(ctx context.Context, email, purpose string) (string, error) {
	if err := InitDB(); err != nil {
		return "", err
	}
	email, err := NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	if purpose != "initial_setup" && purpose != "password_reset" {
		return "", errors.New("invalid account token purpose")
	}

	token, digest, err := randomToken()
	if err != nil {
		return "", err
	}

	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var userID int64
	var passwordHash sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT id, password_hash FROM users WHERE email = $1 FOR UPDATE`, email,
	).Scan(&userID, &passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		if purpose == "password_reset" {
			return "", ErrNoAccount
		}
		err = tx.QueryRowContext(ctx,
			`INSERT INTO users (email) VALUES ($1) RETURNING id`, email,
		).Scan(&userID)
		if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else if purpose == "initial_setup" && passwordHash.Valid {
		return "", ErrAccountExists
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE account_tokens SET used_at = NOW()
         WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`,
		userID, purpose,
	); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO account_tokens (user_id, token_hash, purpose, expires_at)
         VALUES ($1, $2, $3, $4)`,
		userID, digest, purpose, time.Now().Add(accountTokenLife),
	); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func ConsumeAccountToken(ctx context.Context, token, password string) (User, string, time.Time, error) {
	if err := ValidatePassword(password); err != nil {
		return User{}, "", time.Time{}, err
	}
	if err := InitDB(); err != nil {
		return User{}, "", time.Time{}, err
	}
	// Reject random or expired tokens before performing intentionally expensive
	// password hashing. The token is a 256-bit bearer secret, not an identifier.
	var tokenExists bool
	if err := DB.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM account_tokens
    JOIN users ON users.id = account_tokens.user_id
    WHERE account_tokens.token_hash = $1
      AND account_tokens.used_at IS NULL
      AND account_tokens.expires_at > NOW()
      AND users.disabled = FALSE
)`, tokenDigest(token)).Scan(&tokenExists); err != nil {
		return User{}, "", time.Time{}, err
	}
	if !tokenExists {
		return User{}, "", time.Time{}, ErrInvalidToken
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return User{}, "", time.Time{}, err
	}

	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	defer tx.Rollback()

	var accountTokenID int64
	var user User
	err = tx.QueryRowContext(ctx, `
SELECT account_tokens.id, users.id, users.email
FROM account_tokens
JOIN users ON users.id = account_tokens.user_id
WHERE account_tokens.token_hash = $1
  AND account_tokens.used_at IS NULL
  AND account_tokens.expires_at > NOW()
  AND users.disabled = FALSE
FOR UPDATE`, tokenDigest(token)).Scan(&accountTokenID, &user.ID, &user.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", time.Time{}, ErrInvalidToken
	}
	if err != nil {
		return User{}, "", time.Time{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`,
		passwordHash, user.ID,
	); err != nil {
		return User{}, "", time.Time{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE account_tokens SET used_at = NOW() WHERE id = $1`, accountTokenID,
	); err != nil {
		return User{}, "", time.Time{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, user.ID); err != nil {
		return User{}, "", time.Time{}, err
	}

	sessionToken, digest, err := randomToken()
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	expires := time.Now().Add(sessionLifetime)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		user.ID, digest, expires,
	); err != nil {
		return User{}, "", time.Time{}, err
	}

	if err := tx.Commit(); err != nil {
		return User{}, "", time.Time{}, err
	}
	return user, sessionToken, expires, nil
}

func Login(ctx context.Context, email, password string) (User, string, time.Time, error) {
	if err := InitDB(); err != nil {
		return User{}, "", time.Time{}, err
	}
	normalized, emailErr := NormalizeEmail(email)
	if emailErr != nil {
		normalized = strings.ToLower(strings.TrimSpace(email))
	}
	identifier := tokenDigest(normalized)
	globalIdentifier := tokenDigest("a-list-tracker-global-login-throttle")

	globalBlocked, err := loginBlocked(ctx, globalIdentifier)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	identifierBlocked, err := loginBlocked(ctx, identifier)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	if globalBlocked || identifierBlocked {
		return User{}, "", time.Time{}, errors.New("too many login attempts; try again later")
	}

	var user User
	var passwordHash sql.NullString
	var disabled bool
	err = DB.QueryRowContext(ctx,
		`SELECT id, email, password_hash, disabled FROM users WHERE email = $1`, normalized,
	).Scan(&user.ID, &user.Email, &passwordHash, &disabled)
	shouldVerify := err == nil && passwordHash.Valid && !disabled && emailErr == nil
	valid := shouldVerify && VerifyPassword(password, passwordHash.String)
	if !shouldVerify {
		consumePasswordWork(password)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, "", time.Time{}, err
	}
	if !valid {
		if err := recordLoginFailure(ctx, globalIdentifier, 100); err != nil {
			return User{}, "", time.Time{}, err
		}
		// Only known accounts receive their own throttle row. This prevents an
		// attacker from growing the table with arbitrary email addresses.
		if err == nil {
			if err := recordLoginFailure(ctx, identifier, 10); err != nil {
				return User{}, "", time.Time{}, err
			}
		}
		return User{}, "", time.Time{}, errors.New("invalid email or password")
	}

	if _, err := DB.ExecContext(ctx, `DELETE FROM login_throttles WHERE identifier_hash = $1`, identifier); err != nil {
		return User{}, "", time.Time{}, err
	}
	token, digest, err := randomToken()
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	expires := time.Now().Add(sessionLifetime)
	if _, err := DB.ExecContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		user.ID, digest, expires,
	); err != nil {
		return User{}, "", time.Time{}, err
	}
	return user, token, expires, nil
}

func loginBlocked(ctx context.Context, identifier []byte) (bool, error) {
	var blockedUntil sql.NullTime
	err := DB.QueryRowContext(ctx,
		`SELECT blocked_until FROM login_throttles WHERE identifier_hash = $1`, identifier,
	).Scan(&blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return blockedUntil.Valid && blockedUntil.Time.After(time.Now()), nil
}

func recordLoginFailure(ctx context.Context, identifier []byte, threshold int) error {
	_, err := DB.ExecContext(ctx, `
INSERT INTO login_throttles (identifier_hash, failure_count, last_failed_at)
VALUES ($1, 1, NOW())
ON CONFLICT (identifier_hash) DO UPDATE SET
    failure_count = CASE
        WHEN login_throttles.last_failed_at < NOW() - INTERVAL '15 minutes' THEN 1
        ELSE login_throttles.failure_count + 1
    END,
    last_failed_at = NOW(),
    blocked_until = CASE
        WHEN login_throttles.last_failed_at >= NOW() - INTERVAL '15 minutes'
         AND login_throttles.failure_count + 1 >= $2
        THEN NOW() + INTERVAL '10 minutes'
        ELSE NULL
    END`, identifier, threshold)
	return err
}

func AuthenticateRequest(r *http.Request) (User, bool, error) {
	if err := InitDB(); err != nil {
		return User{}, false, err
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return User{}, false, nil
	}

	var user User
	err = DB.QueryRowContext(r.Context(), `
SELECT users.id, users.email
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = $1
	  AND sessions.expires_at > NOW()
  AND users.disabled = FALSE`, tokenDigest(cookie.Value)).Scan(&user.ID, &user.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	return user, true, nil
}

func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok, err := AuthenticateRequest(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"Authentication service unavailable"}`))
			return
		}
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Authentication required"}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

func DeleteCurrentSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenDigest(token))
	return err
}

func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
