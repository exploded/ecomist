-- Capture a customer sign-off on a completed run sheet: a PNG signature
-- (stored as a data URL), the printed name of the signer, and when it was
-- signed. Empty signature means "not yet signed".

ALTER TABLE run_sheets ADD COLUMN signature TEXT NOT NULL DEFAULT '';
ALTER TABLE run_sheets ADD COLUMN signed_by  TEXT NOT NULL DEFAULT '';
ALTER TABLE run_sheets ADD COLUMN signed_at  TEXT;
