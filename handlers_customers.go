package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/exploded/ecomist/internal/db"
)

// --- Customers ---------------------------------------------------------------

func (a *app) customerList(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	like := "%" + q + "%"
	customers, err := a.q.ListCustomersByFranchise(r.Context(), db.ListCustomersByFranchiseParams{
		FranchiseID: cur.FranchiseID,
		Column2:     q,
		Name:        like,
		Suburb:      like,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "Customers")
	pd.Items = customers
	pd.Extra["Query"] = q
	// HTMX search-as-you-type swaps just the results list.
	a.render(w, r, "customers/list", "customers/_list", pd)
}

func (a *app) customerCreate(w http.ResponseWriter, r *http.Request) {
	cur := auth.FromContext(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		toast(w, "Please enter a customer name", "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	runID := formNullInt(r, "run_id")
	err := a.q.CreateCustomer(r.Context(), db.CreateCustomerParams{
		FranchiseID: cur.FranchiseID,
		RunID:       runID,
		RunID_2:     runID,
		Name:        name,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	c, err := a.q.GetLastCustomer(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("HX-Redirect", "/customers/"+itoa(c.ID))
	w.WriteHeader(http.StatusOK)
}

// customerShow renders the single-page editor: details + contacts + zones +
// dispensers, everything editable in place with autosave.
func (a *app) customerShow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := a.customerForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	pd := a.pageData(r, c.Name)
	pd.Item = c
	pd.Extra["CustomerID"] = c.ID
	if err := a.loadCustomerChildren(r, &pd, c.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if c.RunID.Valid {
		if run, err := a.q.GetRun(r.Context(), c.RunID.Int64); err == nil {
			pd.Extra["Run"] = run
		}
	}
	a.render(w, r, "customers/show", "", pd)
}

// loadCustomerChildren fills Extra with contacts, zones, dispensers and tally.
func (a *app) loadCustomerChildren(r *http.Request, pd *PageData, customerID int64) error {
	contacts, err := a.q.ListContactsByCustomer(r.Context(), customerID)
	if err != nil {
		return err
	}
	zones, err := a.q.ListZonesByCustomer(r.Context(), customerID)
	if err != nil {
		return err
	}
	dispensers, err := a.q.ListDispensersByCustomer(r.Context(), customerID)
	if err != nil {
		return err
	}
	tally, err := a.q.CustomerModelTally(r.Context(), customerID)
	if err != nil {
		return err
	}
	pd.Extra["Contacts"] = contacts
	pd.Extra["Zones"] = zones
	pd.Extra["Dispensers"] = groupDispensers(zones, dispensers)
	pd.Extra["Tally"] = tally
	pd.Extra["TotalUnits"] = sumTally(tally)
	return nil
}

// DispenserGroup is a zone (or the no-zone bucket) with its dispensers, used
// by both the customer editor and the run-sheet stop view.
type DispenserGroup struct {
	Zone       *db.Zone
	UID        string // unique suffix for element ids within the page
	Dispensers []db.ListDispensersByCustomerRow
}

// groupDispensers buckets dispensers under their zones (query returns them
// already ordered by zone then sort_order). Zones with no dispensers still
// appear so new units can be added to them.
func groupDispensers(zones []db.Zone, dispensers []db.ListDispensersByCustomerRow) []DispenserGroup {
	byZone := map[int64]int{}
	var groups []DispenserGroup
	for i := range zones {
		byZone[zones[i].ID] = len(groups)
		groups = append(groups, DispenserGroup{Zone: &zones[i], UID: "z" + itoa(zones[i].ID)})
	}
	noZone := DispenserGroup{UID: "z0"}
	for _, d := range dispensers {
		if d.ZoneID.Valid {
			if gi, ok := byZone[d.ZoneID.Int64]; ok {
				groups[gi].Dispensers = append(groups[gi].Dispensers, d)
				continue
			}
		}
		noZone.Dispensers = append(noZone.Dispensers, d)
	}
	if len(noZone.Dispensers) > 0 || len(groups) == 0 {
		groups = append(groups, noZone)
	}
	return groups
}

func sumTally(tally []db.CustomerModelTallyRow) int64 {
	var n int64
	for _, t := range tally {
		n += t.Units
	}
	return n
}

// customerUpdate is the autosave endpoint: applies whichever fields are
// present in the form and saves. Returns 204 - the input keeps its value.
func (a *app) customerUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := a.customerForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	set := func(name string, dst *string) {
		if r.PostForm.Has(name) {
			*dst = strings.TrimSpace(r.PostForm.Get(name))
		}
	}
	set("name", &c.Name)
	set("address_line", &c.AddressLine)
	set("suburb", &c.Suburb)
	set("phone", &c.Phone)
	set("map_ref", &c.MapRef)
	set("regarding", &c.Regarding)
	set("access_notes", &c.AccessNotes)
	set("general_notes", &c.GeneralNotes)
	if r.PostForm.Has("service_minutes") {
		c.ServiceMinutes = formInt(r, "service_minutes", c.ServiceMinutes)
	}
	if err := a.q.UpdateCustomer(r.Context(), db.UpdateCustomerParams{
		Name: c.Name, AddressLine: c.AddressLine, Suburb: c.Suburb, Phone: c.Phone,
		MapRef: c.MapRef, Regarding: c.Regarding, ServiceMinutes: c.ServiceMinutes,
		AccessNotes: c.AccessNotes, GeneralNotes: c.GeneralNotes, ID: c.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) customerDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := a.customerForCur(r, id); a.handleScopeErr(w, r, err) {
		return
	}
	if err := a.q.DeactivateCustomer(r.Context(), id); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("HX-Redirect", "/customers")
	w.WriteHeader(http.StatusOK)
}

// --- Contacts ------------------------------------------------------------

func (a *app) contactCreate(w http.ResponseWriter, r *http.Request) {
	customerID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := a.customerForCur(r, customerID); a.handleScopeErr(w, r, err) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := a.q.CreateContact(r.Context(), db.CreateContactParams{
		CustomerID: customerID,
		Name:       name,
		Role:       strings.TrimSpace(r.FormValue("role")),
		IsPrimary:  formInt(r, "is_primary", 0),
		Phone:      strings.TrimSpace(r.FormValue("phone")),
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderContacts(w, r, customerID)
}

func (a *app) contactUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := a.contactForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	set := func(name string, dst *string) {
		if r.PostForm.Has(name) {
			*dst = strings.TrimSpace(r.PostForm.Get(name))
		}
	}
	set("name", &c.Name)
	set("role", &c.Role)
	set("phone", &c.Phone)
	set("notes", &c.Notes)
	if r.PostForm.Has("is_primary") {
		c.IsPrimary = formInt(r, "is_primary", c.IsPrimary)
	}
	if err := a.q.UpdateContact(r.Context(), db.UpdateContactParams{
		Name: c.Name, Role: c.Role, IsPrimary: c.IsPrimary,
		Phone: c.Phone, Notes: c.Notes, ID: c.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) contactDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := a.contactForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	if err := a.q.DeleteContact(r.Context(), id); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderContacts(w, r, c.CustomerID)
}

// renderContacts re-renders the contacts section fragment.
func (a *app) renderContacts(w http.ResponseWriter, r *http.Request, customerID int64) {
	contacts, err := a.q.ListContactsByCustomer(r.Context(), customerID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "")
	pd.Extra["Contacts"] = contacts
	pd.Extra["CustomerID"] = customerID
	a.renderNamed(w, r, "customers/show", "customers/_contacts", pd)
}

// --- Zones ---------------------------------------------------------------

func (a *app) zoneCreate(w http.ResponseWriter, r *http.Request) {
	customerID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := a.customerForCur(r, customerID)
	if a.handleScopeErr(w, r, err) {
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	area := strings.TrimSpace(r.FormValue("area"))
	if label == "" && area == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := a.q.CreateZone(r.Context(), db.CreateZoneParams{
		CustomerID:   customerID,
		CustomerID_2: customerID,
		Label:        label,
		Area:         area,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderDispenserSection(w, r, c.ID)
}

// resolveZone returns the zone id for a dispenser form. A non-empty zone_name
// (typed straight into the add-dispenser form) find-or-creates a zone for the
// customer by label; otherwise the hidden zone_id (an existing zone) is used.
func (a *app) resolveZone(r *http.Request, customerID int64) (sql.NullInt64, error) {
	name := strings.TrimSpace(r.FormValue("zone_name"))
	if name == "" {
		return formNullInt(r, "zone_id"), nil
	}
	z, err := a.q.GetZoneByLabel(r.Context(), db.GetZoneByLabelParams{
		CustomerID: customerID, Label: name,
	})
	if err == nil {
		return sqlNullInt(z.ID), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sql.NullInt64{}, err
	}
	if err := a.q.CreateZone(r.Context(), db.CreateZoneParams{
		CustomerID: customerID, CustomerID_2: customerID, Label: name,
	}); err != nil {
		return sql.NullInt64{}, err
	}
	z, err = a.q.GetLastZone(r.Context())
	if err != nil {
		return sql.NullInt64{}, err
	}
	return sqlNullInt(z.ID), nil
}

func (a *app) zoneUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	z, err := a.zoneForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	set := func(name string, dst *string) {
		if r.PostForm.Has(name) {
			*dst = strings.TrimSpace(r.PostForm.Get(name))
		}
	}
	set("label", &z.Label)
	set("area", &z.Area)
	set("access_notes", &z.AccessNotes)
	set("notes", &z.Notes)
	if err := a.q.UpdateZone(r.Context(), db.UpdateZoneParams{
		Label: z.Label, Area: z.Area, AccessNotes: z.AccessNotes, Notes: z.Notes, ID: z.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) zoneDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	z, err := a.zoneForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	if err := a.q.DeleteZone(r.Context(), id); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderDispenserSection(w, r, z.CustomerID)
}

// --- Dispensers ------------------------------------------------------------

func (a *app) dispenserCreate(w http.ResponseWriter, r *http.Request) {
	customerID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := a.customerForCur(r, customerID)
	if a.handleScopeErr(w, r, err) {
		return
	}
	location := strings.TrimSpace(r.FormValue("location"))
	modelID, err := a.resolveLookup(r, "model_id", "new_model_name", "models")
	if err != nil || !modelID.Valid || location == "" {
		toast(w, "A location and model are needed", "error")
		w.WriteHeader(http.StatusOK)
		return
	}
	fragranceID, _ := a.resolveLookup(r, "fragrance_id", "new_fragrance_name", "fragrances")
	zoneID, err := a.resolveZone(r, customerID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if err := a.q.CreateDispenser(r.Context(), db.CreateDispenserParams{
		CustomerID:          customerID,
		ZoneID:              zoneID,
		CustomerID_2:        customerID,
		SeqLabel:            strings.TrimSpace(r.FormValue("seq_label")),
		Location:            location,
		ModelID:             modelID.Int64,
		Quantity:            max(1, formInt(r, "quantity", 1)),
		FragranceID:         fragranceID,
		FragranceNote:       strings.TrimSpace(r.FormValue("fragrance_note")),
		RefillSizeMl:        formNullInt(r, "refill_size_ml"),
		ServiceIntervalDays: formNullInt(r, "service_interval_days"),
		Notes:               strings.TrimSpace(r.FormValue("notes")),
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	// Re-render in whichever context the add happened (run sheet or editor).
	if sheetID := formInt(r, "sheet_id", 0); sheetID != 0 {
		a.renderStopDispensers(w, r, sheetID, formInt(r, "stop_id", 0))
		return
	}
	a.renderDispenserSection(w, r, c.ID)
}

func (a *app) dispenserUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := a.dispenserForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	set := func(name string, dst *string) {
		if r.PostForm.Has(name) {
			*dst = strings.TrimSpace(r.PostForm.Get(name))
		}
	}
	set("seq_label", &d.SeqLabel)
	set("location", &d.Location)
	set("fragrance_note", &d.FragranceNote)
	set("notes", &d.Notes)
	if r.PostForm.Has("quantity") {
		d.Quantity = max(1, formInt(r, "quantity", d.Quantity))
	}
	if r.PostForm.Has("zone_id") {
		d.ZoneID = formNullInt(r, "zone_id")
	}
	if r.PostForm.Has("refill_size_ml") {
		d.RefillSizeMl = formNullInt(r, "refill_size_ml")
	}
	if r.PostForm.Has("service_interval_days") {
		d.ServiceIntervalDays = formNullInt(r, "service_interval_days")
	}
	if r.PostForm.Has("model_id") || r.PostForm.Has("new_model_name") {
		if v, err := a.resolveLookup(r, "model_id", "new_model_name", "models"); err == nil && v.Valid {
			d.ModelID = v.Int64
		}
	}
	if r.PostForm.Has("fragrance_id") || r.PostForm.Has("new_fragrance_name") {
		v, err := a.resolveLookup(r, "fragrance_id", "new_fragrance_name", "fragrances")
		if err == nil {
			d.FragranceID = v
		}
	}
	// Combobox "+ Add new" in patch mode: q carries the typed name, combo_create
	// says which catalog it belongs to.
	if kind := r.PostForm.Get("combo_create"); kind != "" {
		if name := strings.TrimSpace(r.PostForm.Get("q")); name != "" {
			if id, err := a.getOrCreateLookup(r.Context(), kind, name); err == nil {
				if kind == "models" {
					d.ModelID = id
				} else {
					d.FragranceID = sqlNullInt(id)
				}
				toast(w, "Added \""+name+"\"", "success")
			}
		}
	}
	if err := a.q.UpdateDispenser(r.Context(), db.UpdateDispenserParams{
		ZoneID: d.ZoneID, SeqLabel: d.SeqLabel, Location: d.Location,
		ModelID: d.ModelID, Quantity: d.Quantity, FragranceID: d.FragranceID,
		FragranceNote: d.FragranceNote, RefillSizeMl: d.RefillSizeMl,
		ServiceIntervalDays: d.ServiceIntervalDays, Notes: d.Notes, ID: d.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	// Combobox-driven changes re-render the row; plain autosaves return 204.
	if r.PostForm.Has("rerender") {
		a.renderDispenserSection(w, r, d.CustomerID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) dispenserDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := a.dispenserForCur(r, id)
	if a.handleScopeErr(w, r, err) {
		return
	}
	if err := a.q.DeactivateDispenser(r.Context(), id); err != nil {
		a.serverError(w, r, err)
		return
	}
	if sheetID := formInt(r, "sheet_id", 0); sheetID != 0 {
		a.renderStopDispensers(w, r, sheetID, formInt(r, "stop_id", 0))
		return
	}
	a.renderDispenserSection(w, r, d.CustomerID)
}

// renderDispenserSection re-renders the zones+dispensers block of the editor.
func (a *app) renderDispenserSection(w http.ResponseWriter, r *http.Request, customerID int64) {
	c, err := a.q.GetCustomer(r.Context(), customerID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	pd := a.pageData(r, "")
	pd.Item = c
	if err := a.loadCustomerChildren(r, &pd, customerID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderNamed(w, r, "customers/show", "customers/_dispensers", pd)
}
