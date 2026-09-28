-- +goose Up
-- A theme belongs to one organization, but its name was kept unique per
-- owner across all of them, so saving an example in a second organization
-- was refused for a theme the person could not even see from there.
DROP INDEX theme_owner_name_idx;
CREATE UNIQUE INDEX theme_owner_name_idx ON theme (org_id, owner_id, lower(name));

-- +goose Down
DROP INDEX theme_owner_name_idx;
CREATE UNIQUE INDEX theme_owner_name_idx ON theme (owner_id, lower(name));
