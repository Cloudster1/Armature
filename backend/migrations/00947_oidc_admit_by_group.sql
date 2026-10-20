-- +goose Up
-- Somebody in a provider group that holds a role is let in at sign-in or at a
-- partner's token exchange, without a join request (#301). Off until an
-- administrator turns it on, so an installation keeps who it lets in.
ALTER TABLE oidc_provider ADD COLUMN admit_by_group boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE oidc_provider DROP COLUMN IF EXISTS admit_by_group;
