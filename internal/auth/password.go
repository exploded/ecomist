// Package auth handles email+password authentication, email verification and
// session middleware.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/exploded/ecomist/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const (
	SessionDuration   = 30 * 24 * time.Hour // 30 days
	SessionMaxAgeSecs = 30 * 24 * 60 * 60   // for cookie MaxAge
	verifyTokenTTL    = 48 * time.Hour
	// AllowedDomain is the email domain ordinary users must belong to.
	AllowedDomain = "ecomist.com.au"
)

var (
	ErrBadCredentials = errors.New("wrong email or password")
	ErrNotVerified    = errors.New("email not verified")
	ErrBadDomain      = fmt.Errorf("only @%s email addresses can register", AllowedDomain)
	ErrWeakPassword   = errors.New("password must be at least 8 characters")
	ErrAlreadyExists  = errors.New("that email is already registered - try signing in")
	ErrBadToken       = errors.New("that link is invalid or has expired")
)

// IsSecure returns true when cookies should have the Secure flag set (HTTPS).
func IsSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// EmailAllowed reports whether an address may register: the configured admin,
// or anyone on the Ecomist domain.
func EmailAllowed(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if admin := strings.ToLower(os.Getenv("ADMIN_EMAIL")); admin != "" && email == admin {
		return true
	}
	return strings.HasSuffix(email, "@"+AllowedDomain)
}

// Register creates an unverified account and returns a fresh verification
// token for it. If the email already exists but is unverified, the password
// is updated and a new token issued (lets people retry a lost email).
func Register(ctx context.Context, queries *db.Queries, email, name, password string) (db.User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || !EmailAllowed(email) {
		return db.User{}, "", ErrBadDomain
	}
	if len(password) < 8 {
		return db.User{}, "", ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return db.User{}, "", err
	}

	user, err := queries.GetUserByEmail(ctx, email)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := queries.CreateUser(ctx, db.CreateUserParams{
			Email: email, Name: strings.TrimSpace(name), PasswordHash: string(hash),
		}); err != nil {
			return db.User{}, "", err
		}
		user, err = queries.GetUserByEmail(ctx, email)
		if err != nil {
			return db.User{}, "", err
		}
	case err != nil:
		return db.User{}, "", err
	case user.EmailVerified == 1:
		return db.User{}, "", ErrAlreadyExists
	default:
		// Unverified re-registration: refresh the password, reissue the link.
		if err := queries.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
			PasswordHash: string(hash), ID: user.ID,
		}); err != nil {
			return db.User{}, "", err
		}
	}

	token, err := IssueVerifyToken(ctx, queries, user.ID)
	if err != nil {
		return db.User{}, "", err
	}
	return user, token, nil
}

// IssueVerifyToken replaces any outstanding tokens for the user with a new one.
func IssueVerifyToken(ctx context.Context, queries *db.Queries, userID int64) (string, error) {
	if err := queries.DeleteEmailTokensForUser(ctx, userID); err != nil {
		return "", err
	}
	token, err := randomHex(32)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(verifyTokenTTL).UTC().Format(time.DateTime)
	if err := queries.CreateEmailToken(ctx, db.CreateEmailTokenParams{
		Token: token, UserID: userID, Purpose: "verify", ExpiresAt: expires,
	}); err != nil {
		return "", err
	}
	return token, nil
}

// VerifyEmail consumes a token: marks the email verified and applies the
// approval rules (admin email becomes cross-franchise admin; pre-approved
// emails land straight in their franchise).
func VerifyEmail(ctx context.Context, queries *db.Queries, token string) (db.User, error) {
	t, err := queries.GetEmailToken(ctx, token)
	if err != nil || t.Purpose != "verify" {
		return db.User{}, ErrBadToken
	}
	if err := queries.MarkEmailVerified(ctx, t.UserID); err != nil {
		return db.User{}, err
	}
	if err := queries.DeleteEmailTokensForUser(ctx, t.UserID); err != nil {
		return db.User{}, err
	}
	user, err := queries.GetUserByID(ctx, t.UserID)
	if err != nil {
		return db.User{}, err
	}
	return applyApprovalRules(ctx, queries, user)
}

// Login checks the password and that the email has been verified.
func Login(ctx context.Context, queries *db.Queries, email, password string) (db.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := queries.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return db.User{}, ErrBadCredentials
	} else if err != nil {
		return db.User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return db.User{}, ErrBadCredentials
	}
	if user.EmailVerified == 0 {
		return user, ErrNotVerified
	}
	// Approval rules may have changed since verification (e.g. admin added
	// them to the approved list) - re-apply on every login.
	return applyApprovalRules(ctx, queries, user)
}

// applyApprovalRules promotes the admin email and auto-approves pre-approved
// addresses into their franchise.
func applyApprovalRules(ctx context.Context, queries *db.Queries, user db.User) (db.User, error) {
	admin := strings.ToLower(os.Getenv("ADMIN_EMAIL"))
	if admin != "" && strings.EqualFold(user.Email, admin) && user.IsAdmin == 0 {
		if err := queries.MakeUserAdmin(ctx, user.ID); err != nil {
			return db.User{}, err
		}
		return queries.GetUserByID(ctx, user.ID)
	}
	if user.Approved == 0 {
		approved, err := queries.GetApprovedEmail(ctx, user.Email)
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
