-- Sign-off belongs to each business (stop), not the run as a whole: every
-- customer signs when their dispensers are serviced. Move the signature fields
-- from run_sheets onto run_sheet_stops.

ALTER TABLE run_sheet_stops ADD COLUMN signature TEXT NOT NULL DEFAULT '';
ALTER TABLE run_sheet_stops ADD COLUMN signed_by  TEXT NOT NULL DEFAULT '';
ALTER TABLE run_sheet_stops ADD COLUMN signed_at  TEXT;

ALTER TABLE run_sheets DROP COLUMN signature;
ALTER TABLE run_sheets DROP COLUMN signed_by;
ALTER TABLE run_sheets DROP COLUMN signed_at;
