package main

import (
	"net/http"
	"strings"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/exploded/ecomist/internal/db"
)

// requireAdmin returns the current admin user or writes a 403.
func (a *app) requireAdmin(w http.ResponseWriter, r *http.Request) *auth.Current {
	cur := auth.FromContext(r.Context())
	if cur == nil || !cur.IsAdmin() {
		http.Error(w, "Admins only", http.StatusForbidden)
		return nil
	}
	return cur
}

func (a *app) adminShow(w http.ResponseWriter, r *http.Request) {
	if a.requireAdmin(w, r) == nil {
		return
	}
	users, err := a.q.ListUsers(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	approved, err := a.q.ListApprovedEmails(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "Admin")
	pd.Extra["Users"] = users
	pd.Extra["Approved"] = approved
	a.render(w, r, "admin/show", "", pd)
}

func (a *app) adminFranchiseCreate(w http.ResponseWriter, r *http.Request) {
	if a.requireAdmin(w, r) == nil {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		toast(w, "Please enter a franchise name", "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := a.q.CreateFranchise(r.Context(), name); err != nil {
		toast(w, "That franchise already exists", "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("HX-Refresh", "true")
}

func (a *app) adminApprovedCreate(w http.ResponseWriter, r *http.Request) {
	if a.requireAdmin(w, r) == nil {
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	franchiseID := formInt(r, "franchise_id", 0)
	if email == "" || !strings.Contains(email, "@") || franchiseID == 0 {
		toast(w, "Enter an email and pick a franchise", "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := a.q.CreateApprovedEmail(r.Context(), db.CreateApprovedEmailParams{
		Email: email, FranchiseID: franchiseID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("HX-Refresh", "true")
}

func (a *app) adminApprovedDelete(w http.ResponseWriter, r *http.Request) {
	if a.requireAdmin(w, r) == nil {
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		email = r.URL.Query().Get("email")
	}
	if err := a.q.DeleteApprovedEmail(r.Context(), email); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("HX-Refresh", "true")
}

func (a *app) adminUserApprove(w http.ResponseWriter, r *http.Request) {
	if a.requireAdmin(w, r) == nil {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	franchiseID := formInt(r, "franchise_id", 0)
	if franchiseID == 0 {
		toast(w, "Pick a franchise for this user", "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := a.q.ApproveUser(r.Context(), db.ApproveUserParams{
		FranchiseID: sqlNullInt(franchiseID), ID: id,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("HX-Refresh", "true")
}

func (a *app) adminUserDelete(w http.ResponseWriter, r *http.Request) {
	cur := a.requireAdmin(w, r)
	if cur == nil {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if id == cur.User.ID {
		toast(w, "You can't remove yourself", "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := a.q.DeleteUser(r.Context(), id); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("HX-Refresh", "true")
}
