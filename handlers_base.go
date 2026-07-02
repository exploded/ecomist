package main

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/exploded/ecomist/internal/db"
	"github.com/justinas/nosurf"
)

type app struct {
	q        *db.Queries
	rawDB    *sql.DB
	pages    PageTemplates
	partials *template.Template
}

// renderPartial renders a template defined in templates/partials (shared set).
func (a *app) renderPartial(w http.ResponseWriter, r *http.Request, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.partials.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render partial", "name", name, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// PageData is the standard data envelope for all templates.
type PageData struct {
	Title         string
	CSRFToken     string
	Cur           *auth.Current
	FranchiseName string
	Franchises    []db.Franchise // admin switcher
	Items         any
	Item          any
	Errors        map[string]string
	Extra         map[string]any
}

// pageData builds the common envelope for the current request.
func (a *app) pageData(r *http.Request, title string) PageData {
	pd := PageData{
		Title:     title,
		CSRFToken: nosurf.Token(r),
		Cur:       auth.FromContext(r.Context()),
		Extra:     map[string]any{},
	}
	if pd.Cur != nil {
		if f, err := a.q.GetFranchise(r.Context(), pd.Cur.FranchiseID); err == nil {
			pd.FranchiseName = f.Name
		}
		if pd.Cur.IsAdmin() {
			pd.Franchises, _ = a.q.ListFranchises(r.Context())
		}
	}
	return pd
}

// isHTMX returns true when the request originated from HTMX.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// render renders the full page (base layout) for normal requests, or only the
// named fragment for HTMX requests when fragment is non-empty.
func (a *app) render(w http.ResponseWriter, r *http.Request, pageName, fragment string, data any) {
	name := "base"
	if isHTMX(r) && fragment != "" {
		name = fragment
	}
	a.renderNamed(w, r, pageName, name, data)
}

// renderNamed renders a specific template name from a page's template set.
func (a *app) renderNamed(w http.ResponseWriter, r *http.Request, pageName, name string, data any) {
	tmpl, ok := a.pages[pageName]
	if !ok {
		slog.Error("unknown page template", "page", pageName)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render", "page", pageName, "name", name, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// serverError logs err and returns a 500.
func (a *app) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("handler", "path", r.URL.Path, "err", err)
	http.Error(w, "Something went wrong", http.StatusInternalServerError)
}

// toast sets an HX-Trigger header that pops a toast (ASCII only - HTTP header).
func toast(w http.ResponseWriter, msg, typ string) {
	w.Header().Set("HX-Trigger", fmt.Sprintf(`{"showToast": {"msg": %q, "type": %q}}`, msg, typ))
}

// pathID parses the {name} path segment as an int64.
func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

var errForbidden = errors.New("not found in this franchise")

// --- Franchise scope checks --------------------------------------------------
// Every object handler resolves its entity through one of these, which verify
// the row belongs to the requester's effective franchise.

func (a *app) customerForCur(r *http.Request, id int64) (db.Customer, error) {
	cur := auth.FromContext(r.Context())
	c, err := a.q.GetCustomer(r.Context(), id)
	if err != nil {
		return db.Customer{}, err
	}
	if cur == nil || c.FranchiseID != cur.FranchiseID {
		return db.Customer{}, errForbidden
	}
	return c, nil
}

func (a *app) runForCur(r *http.Request, id int64) (db.Run, error) {
	cur := auth.FromContext(r.Context())
	run, err := a.q.GetRun(r.Context(), id)
	if err != nil {
		return db.Run{}, err
	}
	if cur == nil || run.FranchiseID != cur.FranchiseID {
		return db.Run{}, errForbidden
	}
	return run, nil
}

func (a *app) sheetForCur(r *http.Request, id int64) (db.GetRunSheetRow, error) {
	cur := auth.FromContext(r.Context())
	s, err := a.q.GetRunSheet(r.Context(), id)
	if err != nil {
		return db.GetRunSheetRow{}, err
	}
	if cur == nil || s.FranchiseID != cur.FranchiseID {
		return db.GetRunSheetRow{}, errForbidden
	}
	return s, nil
}

func (a *app) contactForCur(r *http.Request, id int64) (db.Contact, error) {
	c, err := a.q.GetContact(r.Context(), id)
	if err != nil {
		return db.Contact{}, err
	}
	if _, err := a.customerForCur(r, c.CustomerID); err != nil {
		return db.Contact{}, err
	}
	return c, nil
}

func (a *app) zoneForCur(r *http.Request, id int64) (db.Zone, error) {
	z, err := a.q.GetZone(r.Context(), id)
	if err != nil {
		return db.Zone{}, err
	}
	if _, err := a.customerForCur(r, z.CustomerID); err != nil {
		return db.Zone{}, err
	}
	return z, nil
}

func (a *app) dispenserForCur(r *http.Request, id int64) (db.GetDispenserRow, error) {
	d, err := a.q.GetDispenser(r.Context(), id)
	if err != nil {
		return db.GetDispenserRow{}, err
	}
	if _, err := a.customerForCur(r, d.CustomerID); err != nil {
		return db.GetDispenserRow{}, err
	}
	return d, nil
}

// handleScopeErr writes the right response for a scope-check failure and
// reports whether an error was handled.
func (a *app) handleScopeErr(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, errForbidden):
		http.NotFound(w, r)
		return true
	default:
		a.serverError(w, r, err)
		return true
	}
}

func sqlNullInt(n int64) sql.NullInt64 { return sql.NullInt64{Int64: n, Valid: true} }

// formInt parses an optional integer form value; empty returns invalid NullInt64.
func formNullInt(r *http.Request, name string) sql.NullInt64 {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return sql.NullInt64{}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: n, Valid: true}
}

func formInt(r *http.Request, name string, fallback int64) int64 {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
