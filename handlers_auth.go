package main

import (
	"database/sql"
	"net/http"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/exploded/ecomist/internal/db"
)

func (a *app) loginPage(w http.ResponseWriter, r *http.Request) {
	// Already signed in? Straight to the app.
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
