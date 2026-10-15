-- +goose Up
-- A desk's business hours can take their days off from a holiday calendar
-- (#77), beside the desk's own list. Its days are read whenever a clock is,
-- so a day added there counts from then on; a calendar a desk uses stays
-- until the desk chooses another.
ALTER TABLE business_calendar
    ADD COLUMN holiday_calendar_id uuid REFERENCES holiday_calendar(id) ON DELETE RESTRICT;

-- Deleting a calendar looks for the desks that use it.
CREATE INDEX business_calendar_holiday_calendar_idx ON business_calendar (holiday_calendar_id)
    WHERE holiday_calendar_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS business_calendar_holiday_calendar_idx;
ALTER TABLE business_calendar DROP COLUMN IF EXISTS holiday_calendar_id;
