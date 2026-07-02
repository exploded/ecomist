package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/exploded/ecomist/internal/db"
)

// --- Starting & viewing a run sheet -------------------------------------------

// sheetStart begins a run: creates the sheet plus a frozen stop list. If the
// run already has an open sheet, resume it instead of double-creating.
func (a *app) sheetStart(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	runID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	run, err := a.runForCur(r, runID)
	if a.handleScopeErr(w, r, err) {
		return
	}

	if open, err := a.q.GetOpenRunSheetForRun(r.Context(), run.ID); err == nil {
		a.redirect(w, r, "/sheets/"+itoa(open.ID))
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		a.serverError(w, r, err)
		return
	}

	tx, err := a.rawDB.BeginTx(r.Context(), nil)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	qtx := a.q.WithTx(tx)

	today := time.Now().In(appTZ).Format("2006-01-02")
	if err := qtx.CreateRunSheet(r.Context(), db.CreateRunSheetParams{
		RunID: run.ID, RunDate: today, CreatedBy: cur.User.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	sheet, err := qtx.GetLastRunSheet(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := qtx.CreateStopsForSheet(r.Context(), db.CreateStopsForSheetParams{
		RunSheetID: sheet.ID, RunID: sqlNullInt(run.ID),
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.redirect(w, r, "/sheets/"+itoa(sheet.ID))
}

func (a *app) redirect(w http.ResponseWriter, r *http.Request, url string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// sheetShow is the stop list: progress plus one big card per customer.
func (a *app) sheetShow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sheet, err := a.sheetForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	stops, err := a.q.ListStops(r.Context(), sheet.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var done int64
	for _, s := range stops {
		if s.Status != "pending" {
			done++
		}
	}
	pd := a.pageData(r, sheet.RunName)
	pd.Item = sheet
	pd.Items = stops
	pd.Extra["Done"] = done
	pd.Extra["Total"] = int64(len(stops))
	a.render(w, r, "sheets/show", "", pd)
}

func (a *app) sheetComplete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := a.sheetForCur(r, id); a.handleScopeErr(w, r, err) {
		return
	}
	if err := a.q.CompleteRunSheet(r.Context(), id); err != nil {
		a.serverError(w, r, err)
		return
	}
	toast(w, "Run completed - nice work!", "success")
	a.redirect(w, r, "/")
}

func (a *app) sheetReopen(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := a.sheetForCur(r, id); a.handleScopeErr(w, r, err) {
		return
	}
	if err := a.q.ReopenRunSheet(r.Context(), id); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.redirect(w, r, "/sheets/"+itoa(id))
}

// --- Stop view -----------------------------------------------------------------

// StopDispenserGroup buckets a stop's dispensers under zone headings.
type StopDispenserGroup struct {
	ZoneID          sql.NullInt64
	ZoneLabel       string
	ZoneArea        string
	ZoneAccessNotes string
	Items           []db.ListStopDispensersRow
}

func groupStopDispensers(rows []db.ListStopDispensersRow) []StopDispenserGroup {
	var groups []StopDispenserGroup
	for _, d := range rows {
		if len(groups) == 0 || !nullEq(groups[len(groups)-1].ZoneID, d.ZoneID) {
			groups = append(groups, StopDispenserGroup{
				ZoneID:          d.ZoneID,
				ZoneLabel:       d.ZoneLabel.String,
				ZoneArea:        d.ZoneArea.String,
				ZoneAccessNotes: d.ZoneAccessNotes.String,
			})
		}
		g := &groups[len(groups)-1]
		g.Items = append(g.Items, d)
	}
	return groups
}

func nullEq(a, b sql.NullInt64) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.Int64 == b.Int64
}

// stopForCur loads a stop, verifying it belongs to the sheet in the URL and
// that the sheet is in the requester's franchise.
func (a *app) stopForCur(r *http.Request, sheetID, stopID int64) (db.GetStopRow, db.GetRunSheetRow, error) {
	sheet, err := a.sheetForCur(r, sheetID)
	if err != nil {
		return db.GetStopRow{}, db.GetRunSheetRow{}, err
	}
	stop, err := a.q.GetStop(r.Context(), stopID)
	if err != nil {
		return db.GetStopRow{}, db.GetRunSheetRow{}, err
	}
	if stop.RunSheetID != sheet.ID {
		return db.GetStopRow{}, db.GetRunSheetRow{}, errForbidden
	}
	return stop, sheet, nil
}

func (a *app) stopShow(w http.ResponseWriter, r *http.Request) {
	sheetID, err1 := pathID(r, "id")
	stopID, err2 := pathID(r, "stopID")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	stop, sheet, err := a.stopForCur(r, sheetID, stopID)
	if a.handleScopeErr(w, r, err) {
		return
	}
	rows, err := a.q.ListStopDispensers(r.Context(), db.ListStopDispensersParams{
		RunSheetID: sheet.ID, CustomerID: stop.CustomerID,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	contacts, err := a.q.ListContactsByCustomer(r.Context(), stop.CustomerID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	zones, err := a.q.ListZonesByCustomer(r.Context(), stop.CustomerID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, stop.CustomerName)
	pd.Item = stop
	pd.Extra["Sheet"] = sheet
	pd.Extra["Groups"] = groupStopDispensers(rows)
	pd.Extra["Contacts"] = contacts
	pd.Extra["Zones"] = zones
	a.render(w, r, "sheets/stop", "", pd)
}

// --- Ticks -----------------------------------------------------------------

// tickToggleCommon validates scope and returns everything the tick handlers need.
func (a *app) tickScope(w http.ResponseWriter, r *http.Request) (sheet db.GetRunSheetRow, disp db.GetDispenserRow, stop db.RunSheetStop, ok bool) {
	sheetID, err1 := pathID(r, "id")
	dispID, err2 := pathID(r, "dispenserID")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	sheet, err := a.sheetForCur(r, sheetID)
	if a.handleScopeErr(w, r, err) {
		return
	}
	disp, err = a.dispenserForCur(r, dispID)
	if a.handleScopeErr(w, r, err) {
		return
	}
	stop, err = a.q.GetStopBySheetAndCustomer(r.Context(), db.GetStopBySheetAndCustomerParams{
		RunSheetID: sheet.ID, CustomerID: disp.CustomerID,
	})
	if a.handleScopeErr(w, r, err) {
		return
	}
	if sheet.Status != "open" {
		toast(w, "This run is completed - reopen it to make changes", "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	return sheet, disp, stop, true
}

func (a *app) tickCreate(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	sheet, disp, stop, ok := a.tickScope(w, r)
	if !ok {
		return
	}
	if err := a.q.CreateTick(r.Context(), db.CreateTickParams{
		RunSheetID: sheet.ID, DispenserID: disp.ID, TickedBy: cur.User.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.afterTickChange(w, r, sheet, disp.ID, stop)
}

func (a *app) tickDelete(w http.ResponseWriter, r *http.Request) {
	sheet, disp, stop, ok := a.tickScope(w, r)
	if !ok {
		return
	}
	if err := a.q.DeleteTick(r.Context(), db.DeleteTickParams{
		RunSheetID: sheet.ID, DispenserID: disp.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.afterTickChange(w, r, sheet, disp.ID, stop)
}

func (a *app) tickNote(w http.ResponseWriter, r *http.Request) {
	sheet, disp, _, ok := a.tickScope(w, r)
	if !ok {
		return
	}
	if err := a.q.UpdateTickNote(r.Context(), db.UpdateTickNoteParams{
		Note: strings.TrimSpace(r.FormValue("note")), RunSheetID: sheet.ID, DispenserID: disp.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// afterTickChange applies the auto-complete rule (stop flips to done when all
// units are ticked, back to pending when one is unticked), then re-renders the
// dispenser row with the stop progress header swapped out-of-band.
func (a *app) afterTickChange(w http.ResponseWriter, r *http.Request, sheet db.GetRunSheetRow, dispID int64, stop db.RunSheetStop) {
	cur := auth.FromContext(r.Context())
	fresh, err := a.q.GetStop(r.Context(), stop.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	total := fresh.TotalUnits
	ticked := fresh.TickedUnits
	switch {
	case fresh.Status == "pending" && total > 0 && ticked >= total:
		if err := a.q.UpdateStopStatus(r.Context(), db.UpdateStopStatusParams{
			Status: "done", Note: fresh.Note, CompletedBy: sqlNullInt(cur.User.ID), ID: fresh.ID,
		}); err != nil {
			a.serverError(w, r, err)
			return
		}
		fresh.Status = "done"
		toast(w, "All dispensers done - stop complete!", "success")
	case fresh.Status == "done" && ticked < total:
		if err := a.q.ReopenStop(r.Context(), fresh.ID); err != nil {
			a.serverError(w, r, err)
			return
		}
		fresh.Status = "pending"
	}

	row, err := a.q.GetStopDispenser(r.Context(), db.GetStopDispenserParams{
		RunSheetID: sheet.ID, ID: dispID,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "")
	pd.Item = fresh
	pd.Extra["Sheet"] = sheet
	pd.Extra["Row"] = row
	pd.Extra["OOB"] = true
	// Primary swap: the dispenser row. OOB: the stop progress header.
	a.renderNamed(w, r, "sheets/stop", "sheets/_tick-response", pd)
}

// renderStopDispensers re-renders the full dispenser list of a stop (after
// adding or removing a dispenser mid-run).
func (a *app) renderStopDispensers(w http.ResponseWriter, r *http.Request, sheetID, stopID int64) {
	stop, sheet, err := a.stopForCur(r, sheetID, stopID)
	if a.handleScopeErr(w, r, err) {
		return
	}
	rows, err := a.q.ListStopDispensers(r.Context(), db.ListStopDispensersParams{
		RunSheetID: sheet.ID, CustomerID: stop.CustomerID,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	zones, err := a.q.ListZonesByCustomer(r.Context(), stop.CustomerID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "")
	pd.Item = stop
	pd.Extra["Sheet"] = sheet
	pd.Extra["Groups"] = groupStopDispensers(rows)
	pd.Extra["Zones"] = zones
	a.renderNamed(w, r, "sheets/stop", "sheets/_groups", pd)
}

// --- Stop status --------------------------------------------------------------

func (a *app) stopScope(w http.ResponseWriter, r *http.Request) (db.GetStopRow, bool) {
	stopID, err := pathID(r, "stopID")
	if err != nil {
		http.NotFound(w, r)
		return db.GetStopRow{}, false
	}
	stop, err := a.q.GetStop(r.Context(), stopID)
	if a.handleScopeErr(w, r, err) {
		return db.GetStopRow{}, false
	}
	if _, err := a.sheetForCur(r, stop.RunSheetID); a.handleScopeErr(w, r, err) {
		return db.GetStopRow{}, false
	}
	return stop, true
}

func (a *app) stopNote(w http.ResponseWriter, r *http.Request) {
	stop, ok := a.stopScope(w, r)
	if !ok {
		return
	}
	if err := a.q.UpdateStopNote(r.Context(), db.UpdateStopNoteParams{
		Note: strings.TrimSpace(r.FormValue("note")), ID: stop.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// stopComplete marks a stop done or skipped with an optional note.
func (a *app) stopComplete(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	stop, ok := a.stopScope(w, r)
	if !ok {
		return
	}
	status := r.FormValue("status")
	if status != "done" && status != "skipped" {
		status = "done"
	}
	note := strings.TrimSpace(r.FormValue("note"))
	if note == "" {
		// hx-prompt answers arrive in the HX-Prompt header.
		note = strings.TrimSpace(r.Header.Get("HX-Prompt"))
	}
	if note == "" {
		note = stop.Note
	}
	if err := a.q.UpdateStopStatus(r.Context(), db.UpdateStopStatusParams{
		Status: status, Note: note, CompletedBy: sqlNullInt(cur.User.ID), ID: stop.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.redirect(w, r, "/sheets/"+itoa(stop.RunSheetID))
}

func (a *app) stopReopen(w http.ResponseWriter, r *http.Request) {
	stop, ok := a.stopScope(w, r)
	if !ok {
		return
	}
	if err := a.q.ReopenStop(r.Context(), stop.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.redirect(w, r, "/sheets/"+itoa(stop.RunSheetID)+"/stops/"+itoa(stop.ID))
}
