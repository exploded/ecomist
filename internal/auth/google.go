// Package auth handles Google OAuth2 authentication and session middleware.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/exploded/ecomist/internal/db"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	SessionDuration   = 30 * 24 * time.Hour // 30 days
	SessionMaxAgeSecs = 30 * 24 * 60 * 60   // for cookie MaxAge
	OAuthStateMaxAge  = 300                 // 5 minutes
)

// IsSecure returns true when cookies should have the Secure flag set (HTTPS).
func IsSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

type GoogleUserInfo struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func OAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("BASE_URL") + "/auth/google/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomHex(16)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   OAuthStateMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, OAuthConfig().AuthCodeURL(state), http.StatusTemporaryRedirect)
}

func HandleCallback(queries *db.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stateCookie, err := r.Cookie("oauth_state")
		if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "No code provided", http.StatusBadRequest)
			return
		}

		oauthCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		token, err := OAuthConfig().Exchange(oauthCtx, code)
		if err != nil {
			http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
			return
		}

		client := OAuthConfig().Client(oauthCtx, token)
		resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if err != nil {
			http.Error(w, "Failed to get user info", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			http.Error(w, fmt.Sprintf("Google userinfo returned status %d", resp.StatusCode), http.StatusInternalServerError)
			return
		}

		var info GoogleUserInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			http.Error(w, "Failed to decode user info", http.StatusInternalServerError)
			return
		}

		user, err := upsertUser(r.Context(), queries, info)
		if err != nil {
			slog.Error("upsert user", "err", err)
			http.Error(w, "Failed to save user", http.StatusInternalServerError)
			return
		}

		if err := CreateSessionCookie(w, r, queries, user); err != nil {
			slog.Error("create session", "err", err)
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		// Clear the oauth state cookie.
		http.SetCookie(w, &http.Cookie{
			Name: "oauth_state", Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: IsSecure(r), SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	}
}

// upsertUser finds or creates the user, refreshes profile fields and applies
// the approval rules: ADMIN_EMAIL becomes a cross-franchise admin; an email on
// the approved list is auto-approved into its franchise.
func upsertUser(ctx context.Context, queries *db.Queries, info GoogleUserInfo) (db.User, error) {
	user, err := queries.GetUserByGoogleID(ctx, info.ID)
	if errors.Is(err, sql.ErrNoRows) {
		if err := queries.CreateUser(ctx, db.CreateUserParams{
			GoogleID: info.ID, Email: info.Email, Name: info.Name, PictureUrl: info.Picture,
		}); err != nil {
			return db.User{}, err
		}
		user, err = queries.GetUserByGoogleID(ctx, info.ID)
		if err != nil {
			return db.User{}, err
		}
	} else if err != nil {
		return db.User{}, err
	} else {
		if err := queries.UpdateUserProfile(ctx, db.UpdateUserProfileParams{
			Name: info.Name, PictureUrl: info.Picture, ID: user.ID,
		}); err != nil {
			return db.User{}, err
		}
	}

	adminEmail := os.Getenv("ADMIN_EMAIL")
	if adminEmail != "" && strings.EqualFold(info.Email, adminEmail) && user.IsAdmin == 0 {
		if err := queries.MakeUserAdmin(ctx, user.ID); err != nil {
			return db.User{}, err
		}
		return queries.GetUserByID(ctx, user.ID)
	}

	if user.Approved == 0 {
		approved, err := queries.GetApprovedEmail(ctx, info.Email)
		if err == nil {
			if err := queries.ApproveUser(ctx, db.ApproveUserParams{
				FranchiseID: sql.NullInt64{Int64: approved.FranchiseID, Valid: true},
				ID:          user.ID,
			}); err != nil {
				return db.User{}, err
			}
			return queries.GetUserByID(ctx, user.ID)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return db.User{}, err
		}
	}
	return user, nil
}

// CreateSessionCookie creates a DB session for user and sets the cookie.
// For admins the session's franchise defaults to the first franchise.
func CreateSessionCookie(w http.ResponseWriter, r *http.Request, queries *db.Queries, user db.User) error {
	sessionID, err := randomHex(32)
	if err != nil {
		return err
	}

	franchiseID := user.FranchiseID
	if !franchiseID.Valid && user.IsAdmin == 1 {
		if franchises, err := queries.ListFranchises(r.Context()); err == nil && len(franchises) > 0 {
			franchiseID = sql.NullInt64{Int64: franchises[0].ID, Valid: true}
		}
	}

	expiresAt := time.Now().Add(SessionDuration).UTC().Format(time.DateTime)
	if err := queries.CreateSession(r.Context(), db.CreateSessionParams{
		ID: sessionID, UserID: user.ID, FranchiseID: franchiseID, ExpiresAt: expiresAt,
	}); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   SessionMaxAgeSecs,
		HttpOnly: true,
		Secure:   IsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func HandleLogout(queries *db.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("session"); err == nil {
			_ = queries.DeleteSession(r.Context(), cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name: "session", Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: IsSecure(r), SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("randomHex: %w", err)
	}
	return hex.EncodeToString(b), nil
}
