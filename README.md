# Ecomist

Run sheets, without the paper.

A mobile-first web app for Ecomist franchise technicians and sales people:
maintain customer/dispenser data, print run sheets, and *perform runs* on a
phone — ticking off each dispenser as it's serviced. Replaces the ACT! CRM
paper run sheet.

## Features

- **Multi-franchise** — every user belongs to a franchise; data is fully
  scoped. The admin (`ADMIN_EMAIL`) sees all franchises and can switch between
  them. Sign-in is email + password: registration is restricted to
  `@ecomist.com.au` addresses (plus the admin), ownership is confirmed via an
  emailed link, and the admin then assigns each person to a franchise (or
  pre-approves their email so they land straight in).
- **Master data with autosave** — customers, contacts, zones, dispensers and
  runs are edited in place; every field saves as you type (no Save buttons).
- **Typeahead-that-creates lookups** — dispenser models and fragrances are
  picked from a typeahead; typing a new name adds it to the catalog on the
  spot, so nobody has to visit a maintenance screen mid-job.
- **Perform a run** — start a run to get a frozen stop list; each stop shows
  door codes and access warnings, a Google Maps link, and big tap targets to
  tick dispensers off (with per-dispenser and per-stop notes, undo, skip).
  A stop completes automatically when everything is ticked.
- **Add/remove dispensers mid-run** — found an unrecorded unit on site? Add it
  without leaving the stop, including new models/fragrances via typeahead.
- **Printable run sheet** — a clean redesign of the ACT! layout, one customer
  per page, with computed model tallies and a sign-off block.
- **PDF import** — upload an ACT! run-sheet PDF and Claude extracts the whole
  thing (customers, zones, dispensers) into a preview you confirm before saving.

## Stack

Go (stdlib mux) + html/template + HTMX 2 · SQLite (modernc, pure Go) via sqlc ·
slog · everything `go:embed`-ed into one binary. Claude API for PDF import.

## Development

```sh
cp .env.example .env   # or create .env with DEV_MODE=1, BASE_URL=http://localhost:8995
go build -o ecomist . && ./ecomist
```

With `DEV_MODE=1` (only honoured when BASE_URL is localhost) the login page
gains a "Dev login" button that signs in a local admin without a password, and
registration shows the verification link on-screen when no SMTP server is
configured — so the whole sign-up flow is testable offline.

- Schema: `migrations/*.sql` (applied automatically at startup)
- Queries: `queries/*.sql` → `sqlc generate` → `internal/db/`
- Tests: `go test ./...`

## Environment

| Variable | Purpose |
|---|---|
| `PORT` / `LISTEN_ADDR` | Listen port (default 8995, bound to 127.0.0.1) |
| `DB_PATH` | SQLite file (default `ecomist.db`) |
| `BASE_URL` | Public URL, used in emailed verification links |
| `ADMIN_EMAIL` | This email becomes the cross-franchise admin on registration |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USER` / `SMTP_PASS` / `EMAIL_FROM` | Outbound email for verification links (e.g. Gmail app password, port 587) |
| `ANTHROPIC_API_KEY` | Enables PDF import (feature hides itself when blank) |
| `TZ` | Display timezone (default `Australia/Melbourne`) |
| `DEV_MODE` | `1` enables local dev login (localhost only) |

## Deployment

Standard mchugh.au pipeline: push to `master` → GitHub Actions builds a static
Linux binary and deploys to the Linode box behind Caddy at
`https://ecomist.mchugh.au` (port 8995).

One-time server provisioning:

```sh
curl -fsSL https://raw.githubusercontent.com/exploded/ecomist/master/scripts/server-setup.sh | sudo bash
# then: edit /var/www/ecomist/.env, add the printed Caddy block,
# sudo systemctl reload caddy && sudo systemctl enable --now ecomist
```

Repo secrets required: `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_PORT`,
`DEPLOY_SSH_KEY`.

## Known limitations

- **No offline mode.** A tick made with no phone signal fails with a toast;
  HTMX retries when you tap again. A PWA offline queue is future work — worth
  knowing for hospital basements.
- Data model details: [docs/data-model.md](docs/data-model.md).
