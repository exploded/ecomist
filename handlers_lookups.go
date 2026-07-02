package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/ecomist/internal/db"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// resolveLookup returns the lookup id for a form submission that may carry
// either an existing id (idField) or a brand-new name to create on the fly
// (newNameField). Creating is get-or-create: names are unique (NOCASE).
func (a *app) resolveLookup(r *http.Request, idField, newNameField, kind string) (sql.NullInt64, error) {
	if name := strings.TrimSpace(r.FormValue(newNameField)); name != "" {
		id, err := a.getOrCreateLookup(r.Context(), kind, name)
		if err != nil {
			return sql.NullInt64{}, err
		}
		return sql.NullInt64{Int64: id, Valid: true}, nil
	}
	return formNullInt(r, idField), nil
}

func (a *app) getOrCreateLookup(ctx context.Context, kind, name string) (int64, error) {
	switch kind {
	case "models":
		m, err := a.q.GetModelByName(ctx, name)
		if err == nil {
			return m.ID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if err := a.q.CreateModel(ctx, name); err != nil {
			return 0, err
		}
		m, err = a.q.GetModelByName(ctx, name)
		return m.ID, err
	case "fragrances":
		f, err := a.q.GetFragranceByName(ctx, name)
		if err == nil {
			return f.ID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if err := a.q.CreateFragrance(ctx, db.CreateFragranceParams{Name: name}); err != nil {
			return 0, err
		}
		f, err = a.q.GetFragranceByName(ctx, name)
		return f.ID, err
	}
	return 0, errors.New("unknown lookup kind " + kind)
}

// comboOption is one entry in the typeahead dropdown.
type comboOption struct {
	ID   int64
	Name string
}

// comboData is everything the combobox partial and its menu need to render.
// The same values round-trip through hx-vals so the search endpoint can
// rebuild option buttons that point back at the right target.
type comboData struct {
	Kind        string // "models" | "fragrances"
	Field       string // hidden input name, e.g. "model_id"
	UID         string // uniquifies element ids when several combos share a page
	Query       string
	Value       string // selected id ("" when none)
	Display     string // selected display name
	Placeholder string
	PatchURL    string // when set, options PATCH here (with field=id) instead of selecting locally
	TargetSel   string // hx-target for PatchURL responses
	Options     []comboOption
	ShowCreate  bool
	CSRFToken   string
}

func comboFromRequest(r *http.Request) comboData {
	return comboData{
		Kind:      r.PathValue("kind"),
		Field:     r.FormValue("field"),
		UID:       r.FormValue("uid"),
		Query:     strings.TrimSpace(r.FormValue("q")),
		PatchURL:  r.FormValue("patch_url"),
		TargetSel: r.FormValue("target_sel"),
	}
}

func (a *app) searchLookup(ctx context.Context, kind, q string) ([]comboOption, bool, error) {
	like := "%" + q + "%"
	var opts []comboOption
	exact := false
	switch kind {
	case "models":
		rows, err := a.q.SearchModels(ctx, like)
		if err != nil {
			return nil, false, err
		}
		for _, m := range rows {
			opts = append(opts, comboOption{ID: m.ID, Name: m.Name})
			if strings.EqualFold(m.Name, q) {
				exact = true
			}
		}
	case "fragrances":
		rows, err := a.q.SearchFragrances(ctx, like)
		if err != nil {
			return nil, false, err
		}
		for _, f := range rows {
			opts = append(opts, comboOption{ID: f.ID, Name: f.Name})
			if strings.EqualFold(f.Name, q) {
				exact = true
			}
		}
	default:
		return nil, false, errors.New("unknown lookup kind " + kind)
	}
	return opts, exact, nil
}

// comboSearch returns the dropdown menu fragment for the current query.
func (a *app) comboSearch(w http.ResponseWriter, r *http.Request) {
	cd := comboFromRequest(r)
	opts, exact, err := a.searchLookup(r.Context(), cd.Kind, cd.Query)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cd.Options = opts
	cd.ShowCreate = cd.Query != "" && !exact
	a.renderPartial(w, r, "combo/_menu", cd)
}

// comboCreate handles picking an option or creating a new one, returning the
// combobox in its "selected" state (hidden input filled in).
func (a *app) comboCreate(w http.ResponseWriter, r *http.Request) {
	cd := comboFromRequest(r)
	var (
		id   int64
		name string
	)
	newName := strings.TrimSpace(r.FormValue("name"))
	if newName == "" && r.FormValue("create") == "1" {
		// The create button includes the search input via hx-include.
		newName = cd.Query
	}
	if v := r.FormValue("id"); v != "" {
		id, _ = strconv.ParseInt(v, 10, 64)
		nm, err := a.lookupName(r.Context(), cd.Kind, id)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		name = nm
	} else if nm := newName; nm != "" {
		nid, err := a.getOrCreateLookup(r.Context(), cd.Kind, nm)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		id, name = nid, nm
		toast(w, "Added \""+nm+"\"", "success")
	} else {
		// Cleared selection.
		id, name = 0, ""
	}
	if id != 0 {
		cd.Value = itoa(id)
	}
	cd.Display = name
	a.renderCombo(w, r, cd)
}

// comboSelected re-renders a combobox root (used to reset one).
func (a *app) comboSelected(w http.ResponseWriter, r *http.Request) {
	a.renderCombo(w, r, comboFromRequest(r))
}

func (a *app) renderCombo(w http.ResponseWriter, r *http.Request, cd comboData) {
	a.renderPartial(w, r, "combo", cd)
}

func (a *app) lookupName(ctx context.Context, kind string, id int64) (string, error) {
	switch kind {
	case "models":
		m, err := a.q.GetModel(ctx, id)
		return m.Name, err
	case "fragrances":
		f, err := a.q.GetFragrance(ctx, id)
		return f.Name, err
	}
	return "", errors.New("unknown lookup kind " + kind)
}

// --- Lookup maintenance page ------------------------------------------------

func (a *app) lookupList(w http.ResponseWriter, r *http.Request) {
	models, err := a.q.ListModels(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	fragrances, err := a.q.ListFragrances(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "Lookups")
	pd.Extra["Models"] = models
	pd.Extra["Fragrances"] = fragrances
	a.render(w, r, "lookups/list", "", pd)
}

// lookupUpdate autosaves a lookup row (rename / size / active toggle).
func (a *app) lookupUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	switch r.PathValue("kind") {
	case "models":
		m, err := a.q.GetModel(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if r.PostForm.Has("name") {
			if v := strings.TrimSpace(r.PostForm.Get("name")); v != "" {
				m.Name = v
			}
		}
		if r.PostForm.Has("active") {
			m.Active = formInt(r, "active", m.Active)
		}
		if err := a.q.UpdateModel(r.Context(), db.UpdateModelParams{Name: m.Name, Active: m.Active, ID: m.ID}); err != nil {
			a.serverError(w, r, err)
			return
		}
	case "fragrances":
		f, err := a.q.GetFragrance(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if r.PostForm.Has("name") {
			if v := strings.TrimSpace(r.PostForm.Get("name")); v != "" {
				f.Name = v
			}
		}
		if r.PostForm.Has("default_size_ml") {
			f.DefaultSizeMl = formNullInt(r, "default_size_ml")
		}
		if r.PostForm.Has("active") {
			f.Active = formInt(r, "active", f.Active)
		}
		if err := a.q.UpdateFragrance(r.Context(), db.UpdateFragranceParams{
			Name: f.Name, DefaultSizeMl: f.DefaultSizeMl, Active: f.Active, ID: f.ID,
		}); err != nil {
			a.serverError(w, r, err)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
