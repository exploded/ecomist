// Ecomist run-sheet app: maintain dispenser install data per franchise and
// perform service runs from a phone, replacing the paper ACT! run sheet.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/exploded/ecomist/internal/auth"
	"github.com/exploded/ecomist/internal/db"

	_ "time/tzdata" // embed tzdata: the Linux binary runs on a bare box
)

// appTZ is the timezone used for displaying timestamps (stored as UTC).
var appTZ *time.Location

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := LoadDotEnv(); err != nil {
		slog.Error("load .env", "err", err)
		os.Exit(1)
	}

	tzName := envOr("TZ", "Australia/Melbourne")
	var err error
	appTZ, err = time.LoadLocation(tzName)
	if err != nil {
		slog.Error("load timezone", "tz", tzName, "err", err)
		os.Exit(1)
	}

	dbPath := envOr("DB_PATH", "ecomist.db")
	d, err := openDB(dbPath)
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer d.Close()
	if err := migrate(d); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}

	app := &app{
		q:     db.New(d),
		rawDB: d,
	}
	app.pages, app.partials, err = loadTemplates()
	if err != nil {
		slog.Error("load templates", "err", err)
		os.Exit(1)
	}

	addr := envOr("LISTEN_ADDR", "127.0.0.1:"+envOr("PORT", "8995"))
	srv := &http.Server{
		Addr:              addr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("listening", "addr", addr, "db", dbPath, "devMode", auth.DevMode())
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
