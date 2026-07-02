package main

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/exploded/ecomist/internal/db"
)

// loginPage renders the sign-in form (or bounces straight in when a valid
// session cookie exists).
func (a *app) loginPage(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		if _, err := a.q.GetSession(r.Context(), c.Value); err == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	pd := a.pageData(r, "Sign in")
	pd.Extra["DevMode"] = auth.DevMode()
	a.render(w, r, "login", "", pd)
}

// loginSubmit checks the password and starts a session.
func (a *app) loginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	user, err := auth.Login(r.Context(), a.q, email, password)
	switch {
	case errors.Is(err, auth.ErrBadCredentials):
		a.renderLoginError(w, r, email, "Wrong email or password.", false)
		return
	case errors.Is(err, auth.ErrNotVerified):
		a.renderLoginError(w, r, email, "Please confirm your email address first - check your inbox for the link.", true)
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	if err := auth.CreateSessionCookie(w, r, a.q, user); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) renderLoginError(w http.ResponseWriter, r *http.Request, email, msg string, offerResend bool) {
	pd := a.pageData(r, "Sign in")
	pd.Extra["DevMode"] = auth.DevMode()
	pd.Extra["Error"] = msg
	pd.Extra["Email"] = strings.ToLower(strings.TrimSpace(email))
	pd.Extra["OfferResend"] = offerResend
	a.render(w, r, "login", "", pd)
}

// registerPage shows the create-account form.
func (a *app) registerPage(w http.ResponseWriter, r *http.Request) {
	pd := a.pageData(r, "Create account")
	pd.Extra["Domain"] = auth.AllowedDomain
	a.render(w, r, "register", "", pd)
}

// registerSubmit creates the account and sends the verification email.
func (a *app) registerSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	name := r.FormValue("name")
	password := r.FormValue("password")

	user, token, err := auth.Register(r.Context(), a.q, email, name, password)
	if err != nil {
		pd := a.pageData(r, "Create account")
		pd.Extra["Domain"] = auth.AllowedDomain
		pd.Extra["Error"] = err.Error()
		pd.Extra["Email"] = strings.ToLower(strings.TrimSpace(email))
		pd.Extra["Name"] = strings.TrimSpace(name)
		a.render(w, r, "register", "", pd)
		return
	}
	a.sendVerification(w, r, user, token)
}

// resendVerification reissues the link for an unverified account. Always shows
// the same "check your email" page so addresses can't be probed.
func (a *app) resendVerification(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	user, err := a.q.GetUserByEmail(r.Context(), email)
	if err == nil && user.EmailVerified == 0 {
		token, err := auth.IssueVerifyToken(r.Context(), a.q, user.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		a.sendVerification(w, r, user, token)
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "Check your email")
	pd.Extra["Email"] = email
	a.render(w, r, "verify-sent", "", pd)
}

func (a *app) sendVerification(w http.ResponseWriter, r *http.Request, user db.User, token string) {
	link := strings.TrimRight(os.Getenv("BASE_URL"), "/") + "/verify?token=" + token
	if err := auth.SendVerificationEmail(user.Email, user.Name, link); err != nil {
		slog.Error("send verification email", "email", user.Email, "err", err)
		pd := a.pageData(r, "Create account")
		pd.Extra["Domain"] = auth.AllowedDomain
		pd.Extra["Error"] = "We couldn't send the confirmation email just now - please try again in a minute."
		pd.Extra["Email"] = user.Email
		pd.Extra["Name"] = user.Name
		a.render(w, r, "register", "", pd)
		return
	}
	pd := a.pageData(r, "Check your email")
	pd.Extra["Email"] = user.Email
	// Local convenience: with no mail server, surface the link on-screen.
	if auth.DevMode() && !auth.SMTPConfigured() {
		pd.Extra["DevLink"] = link
	}
	a.render(w, r, "verify-sent", "", pd)
}

// verifyEmail consumes the emailed token and signs the user in.
func (a *app) verifyEmail(w http.ResponseWriter, r *http.Request) {
	user, err := auth.VerifyEmail(r.Context(), a.q, r.URL.Query().Get("token"))
	if err != nil {
		a.renderLoginError(w, r, "", "That confirmation link is invalid or has expired. Sign in to request a new one, or register again.", false)
		return
	}
	if err := auth.CreateSessionCookie(w, r, a.q, user); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) pendingPage(w http.ResponseWriter, r *http.Request) {
	pd := a.pageData(r, "Awaiting approval")
	a.render(w, r, "pending", "", pd)
}

func (a *app) dashboard(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	pd := a.pageData(r, "Today")

	open, err := a.q.ListOpenRunSheetsByFranchise(r.Context(), cur.FranchiseID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	runs, err := a.q.ListRunsByFranchise(r.Context(), cur.FranchiseID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	recent, err := a.q.ListRecentCompletedSheets(r.Context(), cur.FranchiseID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd.Extra["OpenSheets"] = open
	pd.Extra["Runs"] = runs
	pd.Extra["Recent"] = recent
	a.render(w, r, "dashboard", "", pd)
}

// switchFranchise lets an admin change which franchise they are viewing.
func (a *app) switchFranchise(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	if !cur.IsAdmin() {
		http.Error(w, "Admins only", http.StatusForbidden)
		return
	}
	id := formInt(r, "franchise_id", 0)
	if _, err := a.q.GetFranchise(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.q.SetSessionFranchise(r.Context(), db.SetSessionFranchiseParams{
		FranchiseID: sql.NullInt64{Int64: id, Valid: true},
		ID:          cur.SessionID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	// Full reload: nearly everything on screen is franchise-scoped.
	if isHTMX(r) {
		w.Header().Set("HX-Refresh", "true")
		return
	}
	ref := r.Header.Get("Referer")
	if ref == "" {
		ref = "/"
	}
	http.Redirect(w, r, ref, http.StatusSeeOther)
}
