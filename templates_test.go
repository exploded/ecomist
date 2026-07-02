package main

import (
	"testing"
)

// TestLoadTemplates verifies every page parses and key fragments are present
// in their page's clone (guards the clone-per-page loader).
func TestLoadTemplates(t *testing.T) {
	pages, partials, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	wantPages := map[string][]string{
		"customers/show": {"customers/_contacts", "customers/_dispensers", "combo"},
		"runs/show":      {"runs/_stops"},
		"sheets/stop":    {"sheets/_groups", "sheets/_disp", "sheets/_progress", "sheets/_tick-response"},
		"dashboard":      {},
		"runs/print":     {},
		"login":          {},
		"admin/show":     {},
		"lookups/list":   {},
		"import/preview": {},
	}
	for page, frags := range wantPages {
		tmpl, ok := pages[page]
		if !ok {
			t.Errorf("page %q missing; have: %v", page, keys(pages))
			continue
		}
		for _, f := range frags {
			if tmpl.Lookup(f) == nil {
				var names []string
				for _, tt := range tmpl.Templates() {
					names = append(names, tt.Name())
				}
				t.Errorf("page %q missing fragment %q; defined: %v", page, f, names)
			}
		}
	}
	for _, name := range []string{"combo", "combo/_menu", "nav", "base"} {
		if partials.Lookup(name) == nil {
			t.Errorf("partials missing %q", name)
		}
	}
}

func keys(m PageTemplates) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
