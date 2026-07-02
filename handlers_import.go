package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/exploded/ecomist/internal/db"
	"github.com/exploded/ecomist/internal/importer"
)

// pendingImports holds extracted PDFs awaiting confirmation, keyed by token.
// In-memory is fine: single process, and re-uploading is cheap.
var pendingImports = struct {
	sync.Mutex
	m map[string]*importer.Data
}{m: map[string]*importer.Data{}}

func (a *app) importShow(w http.ResponseWriter, r *http.Request) {
	pd := a.pageData(r, "Import a run sheet")
	pd.Extra["Enabled"] = importer.Enabled()
	a.render(w, r, "import/upload", "", pd)
}

// importUpload receives the PDF, extracts it with Claude, and shows a preview.
func (a *app) importUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(40 << 20); err != nil {
		a.importError(w, r, "That file is too large (40 MB max).")
		return
	}
	file, header, err := r.FormFile("pdf")
	if err != nil {
		a.importError(w, r, "Please choose a PDF file.")
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
		a.importError(w, r, "That doesn't look like a PDF.")
		return
	}
	pdf, err := io.ReadAll(file)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	data, err := importer.Extract(r.Context(), pdf)
	if err != nil {
		a.importError(w, r, "Couldn't read that run sheet: "+err.Error())
		return
	}

	token, err := randomToken()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pendingImports.Lock()
	pendingImports.m[token] = data
	pendingImports.Unlock()

	pd := a.pageData(r, "Review import")
	pd.Item = data
	pd.Extra["Token"] = token
	pd.Extra["TotalUnits"] = importTotals(data)
	a.render(w, r, "import/preview", "", pd)
}

func (a *app) importError(w http.ResponseWriter, r *http.Request, msg string) {
	pd := a.pageData(r, "Import a run sheet")
	pd.Extra["Enabled"] = importer.Enabled()
	pd.Extra["Error"] = msg
	a.render(w, r, "import/upload", "", pd)
}

func importTotals(d *importer.Data) int64 {
	var n int64
	for _, c := range d.Customers {
		for _, disp := range c.Dispensers {
			n += max(1, disp.Quantity)
		}
	}
	return n
}

// importConfirm inserts the previewed data into the current franchise.
func (a *app) importConfirm(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	token := r.FormValue("token")
	pendingImports.Lock()
	data := pendingImports.m[token]
	delete(pendingImports.m, token)
	pendingImports.Unlock()
	if data == nil {
		a.importError(w, r, "This import expired - please upload the PDF again.")
		return
	}

	tx, err := a.rawDB.BeginTx(r.Context(), nil)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	qtx := a.q.WithTx(tx)

	runID, err := insertImport(r.Context(), qtx, cur.FranchiseID, data)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		a.serverError(w, r, err)
		return
	}
	toast(w, "Imported "+itoa(int64(len(data.Customers)))+" customers", "success")
	a.redirect(w, r, "/runs/"+itoa(runID))
}

// insertImport writes one extracted run sheet into the database.
func insertImport(ctx context.Context, q *db.Queries, franchiseID int64, data *importer.Data) (int64, error) {
	runName := strings.TrimSpace(data.RunName)
	if runName == "" {
		runName = "Imported run"
	}
	if err := q.CreateRun(ctx, db.CreateRunParams{FranchiseID: franchiseID, Name: runName}); err != nil {
		return 0, err
	}
	run, err := q.GetLastRun(ctx)
	if err != nil {
		return 0, err
	}

	for _, c := range data.Customers {
		if err := q.CreateCustomer(ctx, db.CreateCustomerParams{
			FranchiseID: franchiseID,
			RunID:       sqlNullInt(run.ID),
			RunID_2:     sqlNullInt(run.ID),
			Name:        strings.TrimSpace(c.Name),
		}); err != nil {
			return 0, err
		}
		cust, err := q.GetLastCustomer(ctx)
		if err != nil {
			return 0, err
		}
		minutes := c.ServiceMinutes
		if minutes <= 0 {
			minutes = 15
		}
		if err := q.UpdateCustomer(ctx, db.UpdateCustomerParams{
			Name: cust.Name, AddressLine: c.AddressLine, Suburb: c.Suburb, Phone: c.Phone,
			MapRef: c.MapRef, Regarding: "Service Dispensers", ServiceMinutes: minutes,
			AccessNotes: c.AccessNotes, GeneralNotes: c.GeneralNotes, ID: cust.ID,
		}); err != nil {
			return 0, err
		}

		for _, ct := range c.Contacts {
			if strings.TrimSpace(ct.Name) == "" {
				continue
			}
			isPrimary := int64(0)
			if ct.IsPrimary {
				isPrimary = 1
			}
			if err := q.CreateContact(ctx, db.CreateContactParams{
				CustomerID: cust.ID, Name: ct.Name, Role: ct.Role,
				IsPrimary: isPrimary, Phone: ct.Phone,
			}); err != nil {
				return 0, err
			}
		}

		zoneIDs := map[string]int64{}
		for _, z := range c.Zones {
			if err := q.CreateZone(ctx, db.CreateZoneParams{
				CustomerID: cust.ID, CustomerID_2: cust.ID, Label: z.Label, Area: z.Area,
			}); err != nil {
				return 0, err
			}
			zone, err := q.GetLastZone(ctx)
			if err != nil {
				return 0, err
			}
			if z.AccessNotes != "" {
				if err := q.UpdateZone(ctx, db.UpdateZoneParams{
					Label: zone.Label, Area: zone.Area, AccessNotes: z.AccessNotes, Notes: "", ID: zone.ID,
				}); err != nil {
					return 0, err
				}
			}
			zoneIDs[strings.ToUpper(strings.TrimSpace(z.Label))] = zone.ID
		}

		for _, d := range c.Dispensers {
			modelID, err := getOrCreateLookupTx(ctx, q, "models", firstNonEmpty(d.Model, "Unknown"))
			if err != nil {
				return 0, err
			}
			var fragranceID sql.NullInt64
			if name := strings.TrimSpace(d.Fragrance); name != "" {
				id, err := getOrCreateLookupTx(ctx, q, "fragrances", name)
				if err != nil {
					return 0, err
				}
				fragranceID = sqlNullInt(id)
			}
			var zoneID sql.NullInt64
			if id, ok := zoneIDs[strings.ToUpper(strings.TrimSpace(d.ZoneLabel))]; ok && d.ZoneLabel != "" {
				zoneID = sqlNullInt(id)
			}
			if err := q.CreateDispenser(ctx, db.CreateDispenserParams{
				CustomerID: cust.ID, ZoneID: zoneID, CustomerID_2: cust.ID,
				SeqLabel: d.SeqLabel, Location: firstNonEmpty(d.Location, "(unknown location)"),
				ModelID: modelID, Quantity: max(1, d.Quantity),
				FragranceID: fragranceID, FragranceNote: d.FragranceNote,
				RefillSizeMl:        nullIfZero(d.RefillSizeMl),
				ServiceIntervalDays: nullIfZero(d.ServiceIntervalDays),
				Notes:               d.Notes,
			}); err != nil {
				return 0, err
			}
		}
	}
	return run.ID, nil
}

// getOrCreateLookupTx mirrors app.getOrCreateLookup but runs on a tx-bound Queries.
func getOrCreateLookupTx(ctx context.Context, q *db.Queries, kind, name string) (int64, error) {
	name = strings.TrimSpace(name)
	switch kind {
	case "models":
		m, err := q.GetModelByName(ctx, name)
		if err == nil {
			return m.ID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if err := q.CreateModel(ctx, name); err != nil {
			return 0, err
		}
		m, err = q.GetModelByName(ctx, name)
		return m.ID, err
	case "fragrances":
		f, err := q.GetFragranceByName(ctx, name)
		if err == nil {
			return f.ID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if err := q.CreateFragrance(ctx, db.CreateFragranceParams{Name: name}); err != nil {
			return 0, err
		}
		f, err = q.GetFragranceByName(ctx, name)
		return f.ID, err
	}
	return 0, errors.New("unknown lookup kind " + kind)
}

func nullIfZero(n int64) sql.NullInt64 {
	if n <= 0 {
		return sql.NullInt64{}
	}
	return sqlNullInt(n)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
