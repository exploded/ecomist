-- Ecomist run-sheet app: initial schema.
-- Conventions: INTEGER PKs, TEXT timestamps (UTC, datetime('now')), soft-delete
-- via `active` flags on configuration tables. Tallies are always computed with
-- GROUP BY over dispensers - never stored.

-- ---------------------------------------------------------------------------
-- Tenancy & auth
-- ---------------------------------------------------------------------------

CREATE TABLE franchises (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    active     INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE users (
    id             INTEGER PRIMARY KEY,
    email          TEXT NOT NULL UNIQUE COLLATE NOCASE,
    name           TEXT NOT NULL DEFAULT '',
    password_hash  TEXT NOT NULL,
    -- Set once the user clicks the link in their verification email.
    email_verified INTEGER NOT NULL DEFAULT 0,
    -- NULL franchise_id + is_admin=1 means "all franchises" (super admin).
    franchise_id   INTEGER REFERENCES franchises(id),
    is_admin       INTEGER NOT NULL DEFAULT 0,
    approved       INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Single-use email verification links.
CREATE TABLE email_tokens (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL DEFAULT 'verify',
    expires_at TEXT NOT NULL
);
CREATE INDEX idx_email_tokens_user ON email_tokens(user_id);

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Admins can switch franchise; the current selection lives on the session.
    franchise_id INTEGER REFERENCES franchises(id),
    expires_at   TEXT NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

-- Pre-approved sign-ins: when this email logs in it is auto-approved and
-- assigned to the given franchise.
CREATE TABLE approved_emails (
    email        TEXT PRIMARY KEY COLLATE NOCASE,
    franchise_id INTEGER NOT NULL REFERENCES franchises(id)
);

-- ---------------------------------------------------------------------------
-- Reference catalogs (global across franchises)
-- ---------------------------------------------------------------------------

CREATE TABLE dispenser_models (
    id     INTEGER PRIMARY KEY,
    name   TEXT NOT NULL UNIQUE COLLATE NOCASE,
    active INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE fragrances (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE COLLATE NOCASE,
    default_size_ml INTEGER,
    active          INTEGER NOT NULL DEFAULT 1
);

-- ---------------------------------------------------------------------------
-- Configuration (franchise-scoped): the standing install
-- ---------------------------------------------------------------------------

CREATE TABLE runs (
    id           INTEGER PRIMARY KEY,
    franchise_id INTEGER NOT NULL REFERENCES franchises(id),
    name         TEXT NOT NULL,
    notes        TEXT NOT NULL DEFAULT '',
    active       INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_runs_franchise ON runs(franchise_id);

CREATE TABLE customers (
    id              INTEGER PRIMARY KEY,
    franchise_id    INTEGER NOT NULL REFERENCES franchises(id),
    run_id          INTEGER REFERENCES runs(id),
    sort_order      INTEGER NOT NULL DEFAULT 0,
    name            TEXT NOT NULL,
    address_line    TEXT NOT NULL DEFAULT '',
    suburb          TEXT NOT NULL DEFAULT '',
    phone           TEXT NOT NULL DEFAULT '',
    map_ref         TEXT NOT NULL DEFAULT '',
    regarding       TEXT NOT NULL DEFAULT 'Service Dispensers',
    service_minutes INTEGER NOT NULL DEFAULT 15,
    access_notes    TEXT NOT NULL DEFAULT '',
    general_notes   TEXT NOT NULL DEFAULT '',
    active          INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_customers_franchise ON customers(franchise_id);
CREATE INDEX idx_customers_run ON customers(run_id, sort_order);

CREATE TABLE contacts (
    id          INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT '',
    is_primary  INTEGER NOT NULL DEFAULT 0,
    phone       TEXT NOT NULL DEFAULT '',
    notes       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_contacts_customer ON contacts(customer_id);

CREATE TABLE zones (
    id           INTEGER PRIMARY KEY,
    customer_id  INTEGER NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    label        TEXT NOT NULL DEFAULT '',
    area         TEXT NOT NULL DEFAULT '',
    access_notes TEXT NOT NULL DEFAULT '',
    notes        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_zones_customer ON zones(customer_id, sort_order);

CREATE TABLE dispensers (
    id                    INTEGER PRIMARY KEY,
    customer_id           INTEGER NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    zone_id               INTEGER REFERENCES zones(id) ON DELETE SET NULL,
    sort_order            INTEGER NOT NULL DEFAULT 0,
    -- Printed label, can be a range: "1", "9 - 10". quantity carries the maths.
    seq_label             TEXT NOT NULL DEFAULT '',
    location              TEXT NOT NULL,
    model_id              INTEGER NOT NULL REFERENCES dispenser_models(id),
    quantity              INTEGER NOT NULL DEFAULT 1,
    -- NULL fragrance_id means "see fragrance_note" (e.g. "See Below"/"Various").
    fragrance_id          INTEGER REFERENCES fragrances(id),
    fragrance_note        TEXT NOT NULL DEFAULT '',
    refill_size_ml        INTEGER,
    service_interval_days INTEGER,
    notes                 TEXT NOT NULL DEFAULT '',
    active                INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_dispensers_customer ON dispensers(customer_id, sort_order);

-- ---------------------------------------------------------------------------
-- Operational: performing a run
-- ---------------------------------------------------------------------------

CREATE TABLE run_sheets (
    id           INTEGER PRIMARY KEY,
    run_id       INTEGER NOT NULL REFERENCES runs(id),
    run_date     TEXT NOT NULL, -- YYYY-MM-DD
    status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'completed')),
    created_by   INTEGER NOT NULL REFERENCES users(id),
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT
);
CREATE INDEX idx_run_sheets_run ON run_sheets(run_id);

-- Stop list is frozen when the sheet starts (one row per active customer).
CREATE TABLE run_sheet_stops (
    id           INTEGER PRIMARY KEY,
    run_sheet_id INTEGER NOT NULL REFERENCES run_sheets(id) ON DELETE CASCADE,
    customer_id  INTEGER NOT NULL REFERENCES customers(id),
    sort_order   INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'skipped')),
    note         TEXT NOT NULL DEFAULT '',
    completed_at TEXT,
    completed_by INTEGER REFERENCES users(id),
    UNIQUE (run_sheet_id, customer_id)
);
CREATE INDEX idx_stops_sheet ON run_sheet_stops(run_sheet_id, sort_order);

-- Tick = INSERT, untick = DELETE.
CREATE TABLE run_sheet_ticks (
    id           INTEGER PRIMARY KEY,
    run_sheet_id INTEGER NOT NULL REFERENCES run_sheets(id) ON DELETE CASCADE,
    dispenser_id INTEGER NOT NULL REFERENCES dispensers(id) ON DELETE CASCADE,
    ticked_by    INTEGER NOT NULL REFERENCES users(id),
    ticked_at    TEXT NOT NULL DEFAULT (datetime('now')),
    note         TEXT NOT NULL DEFAULT '',
    UNIQUE (run_sheet_id, dispenser_id)
);
CREATE INDEX idx_ticks_sheet ON run_sheet_ticks(run_sheet_id);

-- ---------------------------------------------------------------------------
-- Seed
-- ---------------------------------------------------------------------------

INSERT INTO franchises (name) VALUES ('Ecomist Burwood');
