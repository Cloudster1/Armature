-- +goose Up
-- How much a team can take on in a week, in the unit issues are estimated in.
-- Null is "has not said", which the plan reads differently from zero.
ALTER TABLE team ADD COLUMN weekly_capacity numeric(8,2);
ALTER TABLE team ADD CONSTRAINT team_weekly_capacity_not_negative
    CHECK (weekly_capacity IS NULL OR weekly_capacity >= 0);

-- +goose Down
ALTER TABLE team DROP CONSTRAINT IF EXISTS team_weekly_capacity_not_negative;
ALTER TABLE team DROP COLUMN IF EXISTS weekly_capacity;
