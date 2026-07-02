package main

import (
	"net/http"
	"strings"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/exploded/ecomist/internal/db"
)

func (a *app) runList(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	runs, err := a.q.ListRunsByFranchise(r.Context(), cur.FranchiseID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "Runs")
	pd.Items = runs
	a.render(w, r, "runs/list", "", pd)
}

func (a *app) runCreate(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	if cur.FranchiseID == 0 {
		toast(w, noFranchiseMsg, "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		toast(w, "Please enter a run name", "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := a.q.CreateRun(r.Context(), db.CreateRunParams{
		FranchiseID: cur.FranchiseID, Name: name,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	run, err := a.q.GetLastRun(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("HX-Redirect", "/runs/"+itoa(run.ID))
	w.WriteHeader(http.StatusOK)
}

func (a *app) runShow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	run, err := a.runForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	pd := a.pageData(r, run.Name)
	pd.Item = run
	if err := a.loadRunStops(r, &pd, run.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, "runs/show", "", pd)
}

func (a *app) loadRunStops(r *http.Request, pd *PageData, runID int64) error {
	cur := auth.FromContext(r.Context())
	stops, err := a.q.ListRunCustomers(r.Context(), sqlNullInt(runID))
	if err != nil {
		return err
	}
	unassigned, err := a.q.ListUnassignedCustomers(r.Context(), cur.FranchiseID)
	if err != nil {
		return err
	}
	pd.Extra["Stops"] = stops
	pd.Extra["Unassigned"] = unassigned
	return nil
}

func (a *app) runUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	run, err := a.runForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	if r.PostForm.Has("name") {
		if v := strings.TrimSpace(r.PostForm.Get("name")); v != "" {
			run.Name = v
		}
	}
	if r.PostForm.Has("notes") {
		run.Notes = strings.TrimSpace(r.PostForm.Get("notes"))
	}
	if err := a.q.UpdateRun(r.Context(), db.UpdateRunParams{Name: run.Name, Notes: run.Notes, ID: run.ID}); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) runDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := a.runForCur(r, id); a.handleScopeErr(w, r, err) {
		return
	}
	if err := a.q.DeactivateRun(r.Context(), id); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("HX-Redirect", "/runs")
	w.WriteHeader(http.StatusOK)
}

func (a *app) runAddCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	run, err := a.runForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	customerID := formInt(r, "customer_id", 0)
	if _, err := a.customerForCur(r, customerID); a.handleScopeErr(w, r, err) {
		return
	}
	if err := a.q.AssignCustomerToRun(r.Context(), db.AssignCustomerToRunParams{
		RunID:   sqlNullInt(run.ID),
		RunID_2: sqlNullInt(run.ID),
		ID:      customerID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderRunStops(w, r, run.ID)
}

func (a *app) runRemoveCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	run, err := a.runForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	customerID, err := pathID(r, "customerID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := a.customerForCur(r, customerID); a.handleScopeErr(w, r, err) {
		return
	}
	if err := a.q.RemoveCustomerFromRun(r.Context(), customerID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderRunStops(w, r, run.ID)
}

// runReorder moves a customer up or down within the run's stop order by
// swapping sort_order with its neighbour.
func (a *app) runReorder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	run, err := a.runForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	customerID := formInt(r, "customer_id", 0)
	dir := r.FormValue("dir")

	stops, err := a.q.ListRunCustomers(r.Context(), sqlNullInt(run.ID))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	idx := -1
	for i, s := range stops {
		if s.ID == customerID {
			idx = i
			break
		}
	}
	swap := -1
	if dir == "up" && idx > 0 {
		swap = idx - 1
	} else if dir == "down" && idx >= 0 && idx < len(stops)-1 {
		swap = idx + 1
	}
	if swap >= 0 {
		// Renumber the whole list to keep sort_order dense, then swap.
		stops[idx], stops[swap] = stops[swap], stops[idx]
		for i, s := range stops {
			if err := a.q.SetCustomerSortOrder(r.Context(), db.SetCustomerSortOrderParams{
				SortOrder: int64(i + 1), ID: s.ID,
			}); err != nil {
				a.serverError(w, r, err)
				return
			}
		}
	}
	a.renderRunStops(w, r, run.ID)
}

func (a *app) renderRunStops(w http.ResponseWriter, r *http.Request, runID int64) {
	run, err := a.q.GetRun(r.Context(), runID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "")
	pd.Item = run
	if err := a.loadRunStops(r, &pd, runID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderNamed(w, r, "runs/show", "runs/_stops", pd)
}

// --- Print sheet -------------------------------------------------------------

// printCustomer is one customer's full block on the printed run sheet.
type printCustomer struct {
	Customer   db.ListRunCustomersRow
	Contacts   []db.Contact
	Groups     []DispenserGroup
	Tally      []db.CustomerModelTallyRow
	TotalUnits int64
}

func (a *app) runPrint(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	run, err := a.runForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	customers, err := a.q.ListRunCustomers(r.Context(), sqlNullInt(run.ID))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var blocks []printCustomer
	for _, c := range customers {
		contacts, err := a.q.ListContactsByCustomer(r.Context(), c.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		zones, err := a.q.ListZonesByCustomer(r.Context(), c.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		dispensers, err := a.q.ListDispensersByCustomer(r.Context(), c.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		tally, err := a.q.CustomerModelTally(r.Context(), c.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		blocks = append(blocks, printCustomer{
			Customer:   c,
			Contacts:   contacts,
			Groups:     groupDispensers(zones, dispensers),
			Tally:      tally,
			TotalUnits: sumTally(tally),
		})
	}
	pd := a.pageData(r, run.Name+" - Run Sheet")
	pd.Item = run
	pd.Items = blocks
	pd.Extra["Date"] = r.URL.Query().Get("date")
	a.render(w, r, "runs/print", "", pd)
}
