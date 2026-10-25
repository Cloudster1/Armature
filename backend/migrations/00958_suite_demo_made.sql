-- +goose Up
-- The keys whose projects the suite demo made itself (#415), so a failed demo
-- can be added again over them, and carries on where it stopped.

ALTER TABLE suite_demo ADD COLUMN made text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE suite_demo DROP COLUMN IF EXISTS made;
