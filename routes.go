package main

import (
	"log/slog"
	"net/http"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/justinas/nosurf"
)

func (a *app) routes() http.Handler {
	// Authenticated application routes.
	appMux := http.NewServeMux()
	appMux.HandleFunc("GET /{$}", a.dashboard)
	appMux.HandleFunc("POST /switch-franchise", a.switchFranchise)

	// Customers + children
	appMux.HandleFunc("GET /customers", a.customerList)
	appMux.HandleFunc("POST /customers", a.customerCreate)
	appMux.HandleFunc("GET /customers/{id}", a.customerShow)
	appMux.HandleFunc("PATCH /customers/{id}", a.customerUpdate)
	appMux.HandleFunc("DELETE /customers/{id}", a.customerDelete)
	appMux.HandleFunc("POST /customers/{id}/contacts", a.contactCreate)
	appMux.HandleFunc("PATCH /contacts/{id}", a.contactUpdate)
	appMux.HandleFunc("DELETE /contacts/{id}", a.contactDelete)
	appMux.HandleFunc("POST /customers/{id}/zones", a.zoneCreate)
	appMux.HandleFunc("PATCH /zones/{id}", a.zoneUpdate)
	appMux.HandleFunc("DELETE /zones/{id}", a.zoneDelete)
	appMux.HandleFunc("POST /customers/{id}/dispensers", a.dispenserCreate)
	appMux.HandleFunc("PATCH /dispensers/{id}", a.dispenserUpdate)
	appMux.HandleFunc("DELETE /dispensers/{id}", a.dispenserDelete)

	// Runs
	appMux.HandleFunc("GET /runs", a.runList)
	appMux.HandleFunc("POST /runs", a.runCreate)
	appMux.HandleFunc("GET /runs/new", a.runNew)
	appMux.HandleFunc("POST /runs/from-suburbs", a.runCreateFromSuburbs)
	appMux.HandleFunc("GET /runs/{id}", a.runShow)
	appMux.HandleFunc("PATCH /runs/{id}", a.runUpdate)
	appMux.HandleFunc("DELETE /runs/{id}", a.runDelete)
	appMux.HandleFunc("POST /runs/{id}/customers", a.runAddCustomer)
	appMux.HandleFunc("DELETE /runs/{id}/customers/{customerID}", a.runRemoveCustomer)
	appMux.HandleFunc("POST /runs/{id}/reorder", a.runReorder)
	appMux.HandleFunc("GET /runs/{id}/print", a.runPrint)
	appMux.HandleFunc("POST /runs/{id}/start", a.sheetStart)

	// Lookups + typeahead combobox
	appMux.HandleFunc("GET /lookups", a.lookupList)
	appMux.HandleFunc("PATCH /lookups/{kind}/{id}", a.lookupUpdate)
	appMux.HandleFunc("GET /combo/{kind}", a.comboSearch)
	appMux.HandleFunc("POST /combo/{kind}", a.comboCreate)
	appMux.HandleFunc("GET /combo/{kind}/selected", a.comboSelected)

	// Run sheets (performing a run)
	appMux.HandleFunc("GET /sheets/{id}", a.sheetShow)
	appMux.HandleFunc("POST /sheets/{id}/complete", a.sheetComplete)
	appMux.HandleFunc("POST /sheets/{id}/reopen", a.sheetReopen)
	appMux.HandleFunc("POST /sheets/{id}/reorder", a.stopReorder)
	appMux.HandleFunc("POST /sheets/{id}/sign", a.sheetSign)
	appMux.HandleFunc("POST /sheets/{id}/unsign", a.sheetUnsign)
	appMux.HandleFunc("GET /sheets/{id}/stops/{stopID}", a.stopShow)
	appMux.HandleFunc("POST /sheets/{id}/ticks/{dispenserID}", a.tickCreate)
	appMux.HandleFunc("DELETE /sheets/{id}/ticks/{dispenserID}", a.tickDelete)
	appMux.HandleFunc("PATCH /sheets/{id}/ticks/{dispenserID}/note", a.tickNote)
	appMux.HandleFunc("PATCH /stops/{stopID}/note", a.stopNote)
	appMux.HandleFunc("POST /stops/{stopID}/complete", a.stopComplete)
	appMux.HandleFunc("POST /stops/{stopID}/reopen", a.stopReopen)

	// PDF import
	appMux.HandleFunc("GET /import", a.importShow)
	appMux.HandleFunc("POST /import", a.importUpload)
	appMux.HandleFunc("POST /import/confirm", a.importConfirm)

	// Admin
	appMux.HandleFunc("GET /admin", a.adminShow)
	appMux.HandleFunc("POST /admin/franchises", a.adminFranchiseCreate)
	appMux.HandleFunc("POST /admin/approved", a.adminApprovedCreate)
	appMux.HandleFunc("DELETE /admin/approved", a.adminApprovedDelete)
	appMux.HandleFunc("POST /admin/users/{id}/approve", a.adminUserApprove)
	appMux.HandleFunc("DELETE /admin/users/{id}", a.adminUserDelete)

	protected := auth.RequireAuth(a.q, appMux, http.HandlerFunc(a.pendingPage))

	// Top-level mux: public routes + static + everything else behind auth.
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler()))
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.loginSubmit)
	mux.HandleFunc("GET /register", a.registerPage)
	mux.HandleFunc("POST /register", a.registerSubmit)
	mux.HandleFunc("GET /verify", a.verifyEmail)
	mux.HandleFunc("POST /resend-verification", a.resendVerification)
	mux.Handle("POST /logout", auth.HandleLogout(a.q))
	if auth.DevMode() {
		mux.Handle("GET /dev/login", auth.DevLogin(a.q))
	}
	mux.Handle("/", protected)

	csrf := nosurf.New(mux)
	csrf.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Warn("csrf failure", "reason", nosurf.Reason(r), "path", r.URL.Path)
		http.Error(w, "Security check failed - please refresh the page and try again", http.StatusBadRequest)
	}))
	return logRequests(recoverPanics(csrf))
}
