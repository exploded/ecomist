package main

import (
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// all: is required - plain "templates" would skip _fragment.html files,
// which Go's embed excludes by default (names starting with _ or .).
//
//go:embed all:templates
var templateFS embed.FS

// PageTemplates maps page names ("customers/list") to their cloned template
// set. Each page gets its own copy of layouts + partials so that
// {{define "content"}} doesn't collide across pages.
type PageTemplates map[string]*template.Template

func templateFuncs() template.FuncMap {
	toI64 := func(v any) int64 {
		switch n := v.(type) {
		case int:
			return int64(n)
		case int64:
			return n
		case float64:
			return int64(n)
		}
		return 0
	}
	return template.FuncMap{
		"add": func(a, b any) int64 { return toI64(a) + toI64(b) },
		// dict builds a map for passing multiple named values to a partial.
		"dict": func(pairs ...any) (map[string]any, error) {
			if len(pairs)%2 != 0 {
				return nil, fmt.Errorf("dict: odd number of arguments")
			}
			m := make(map[string]any, len(pairs)/2)
			for i := 0; i < len(pairs); i += 2 {
				k, ok := pairs[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key %v is not a string", pairs[i])
				}
				m[k] = pairs[i+1]
			}
			return m, nil
		},
		// localTime formats a stored UTC "2006-01-02 15:04:05" as local "3:04pm".
		"localTime": func(s string) string {
			t, err := time.ParseInLocation(time.DateTime, s, time.UTC)
			if err != nil {
				return s
			}
			return strings.ToLower(t.In(appTZ).Format("3:04pm"))
		},
		// localDate formats a stored UTC datetime as "Mon 2 Jan".
		"localDate": func(s string) string {
			t, err := time.ParseInLocation(time.DateTime, s, time.UTC)
			if err != nil {
				return s
			}
			return t.In(appTZ).Format("Mon 2 Jan")
		},
		// niceDate formats a plain "2006-01-02" date as "Friday, 26 June 2026".
		"niceDate": func(s string) string {
			t, err := time.Parse("2006-01-02", s)
			if err != nil {
				return s
			}
			return t.Format("Monday, 2 January 2006")
		},
		// mapsURL builds a Google Maps search link for an address.
		"mapsURL": func(address, suburb string) string {
			q := strings.TrimSpace(strings.TrimSpace(address) + " " + strings.TrimSpace(suburb))
			if q == "" {
				return ""
			}
			return "https://www.google.com/maps/search/?api=1&query=" + url.QueryEscape(q)
		},
		// singular trims the plural s from a lookup kind ("models" -> "model").
		"singular": func(s string) string { return strings.TrimSuffix(s, "s") },
		"firstName": func(s string) string {
			name, _, _ := strings.Cut(strings.TrimSpace(s), " ")
			return name
		},
		// nullIntStr renders a nullable FK as its id string, or "" when unset.
		"nullIntStr": func(n sql.NullInt64) string {
			if !n.Valid {
				return ""
			}
			return strconv.FormatInt(n.Int64, 10)
		},
		"pct": func(done, total int64) int64 {
			if total == 0 {
				return 0
			}
			return done * 100 / total
		},
	}
}

// loadTemplates builds a shared base of layouts + partials, then clones it per
// page so each page's {{define "content"}} is isolated (see go-htmx skill).
// The base set is also returned so partials (e.g. the combobox) can be rendered
// standalone by fragment endpoints.
func loadTemplates() (PageTemplates, *template.Template, error) {
	base := template.New("").Funcs(templateFuncs())
	base, err := base.ParseFS(templateFS, "templates/layouts/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, nil, fmt.Errorf("parse layouts/partials: %w", err)
	}

	pages := make(PageTemplates)
	err = fs.WalkDir(templateFS, "templates/pages", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		rel := strings.TrimPrefix(p, "templates/pages/")
		name := strings.TrimSuffix(rel, ".html")

		clone, err := base.Clone()
		if err != nil {
			return err
		}
		t, err := clone.ParseFS(templateFS, p)
		if err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		// Parse sibling _fragments into the same clone so pages can render them.
		frags, err := fs.Glob(templateFS, path.Dir(p)+"/_*.html")
		if err != nil {
			return err
		}
		if len(frags) > 0 {
			if t, err = t.ParseFS(templateFS, frags...); err != nil {
				return fmt.Errorf("parse fragments for %s: %w", p, err)
			}
		}
		pages[name] = t
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return pages, base, nil
}
