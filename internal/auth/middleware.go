package auth

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/exploded/ecomist/internal/db"
)

type contextKey string

const currentKey contextKey = "current"

// Current carries the authenticated user plus their effective franchise for
// this request. Admins have no franchise of their own; their session stores
// which franchise they are currently viewing.
type Current struct {
	User        db.User
	SessionID   string
	FranchiseID int64 // effective franchise for all data queries
}

func (c *Current) IsAdmin() bool { return c.User.IsAdmin == 1 }

// FromContext returns the Current for the request, or nil when unauthenticated.
func FromContext(ctx context.Context) *Current {
	c, _ := ctx.Value(currentKey).(*Current)
	return c
}

// DevMode reports whether the local dev-login bypass is enabled. It refuses to
// activate unless BASE_URL points at localhost, so it can never be switched on
// in production by accident.
func DevMode() bool {
	if os.Getenv("DEV_MODE") != "1" {
		return false
	}
	base := os.Getenv("BASE_URL")
	return strings.Contains(base, "localhost") || strings.Contains(base, "127.0.0.1")
}

// RequireAuth wraps next so only approved users get through. Unauthenticated
// requests are redirected to /login; authenticated-but-unapproved users see
// pendingHandler.
func RequireAuth(queries *db.Queries, next http.Handler, pendingHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			redirectToLogin(w, r)
			return
		}

		session, err := queries.GetSession(r.Context(), cookie.Value)
		if err != nil {
			// Session expired or invalid.
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
			redirectToLogin(w, r)
			return
		}

		user, err := queries.GetUserByID(r.Context(), session.UserID)
		if err != nil {
			redirectToLogin(w, r)
			return
		}

		cur := &Current{User: user, SessionID: session.ID}
		switch {
		case user.FranchiseID.Valid:
			cur.FranchiseID = user.FranchiseID.Int64
		case session.FranchiseID.Valid:
			cur.FranchiseID = session.FranchiseID.Int64
		}
		ctx := context.WithValue(r.Context(), currentKey, cur)

		if user.Approved == 0 {
			pendingHandler.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// redirectToLogin sends browsers to /login; HTMX requests get an HX-Redirect
// so the whole page navigates rather than swapping in the login form.
func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
}

// DevLogin creates (once) and signs in a local admin user. Only reachable when
// DevMode() is true — the route is not even registered otherwise.
func DevLogin(queries *db.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !DevMode() {
			http.NotFound(w, r)
			return
		}
		const devGoogleID = "dev-local-admin"
		user, err := queries.GetUserByGoogleID(r.Context(), devGoogleID)
		if errors.Is(err, sql.ErrNoRows) {
			if err := queries.CreateUser(r.Context(), db.CreateUserParams{
				GoogleID: devGoogleID, Email: "dev@localhost", Name: "Dev Admin", PictureUrl: "",
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			user, err = queries.GetUserByGoogleID(r.Context(), devGoogleID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := queries.MakeUserAdmin(r.Context(), user.ID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			user, _ = queries.GetUserByID(r.Context(), user.ID)
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := CreateSessionCookie(w, r, queries, user); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("dev login", "user", user.Email)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}
