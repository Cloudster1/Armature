-- +goose Up
-- A desk may name the domains it takes requests from; an empty list is
-- everyone. Lower case and without a leading @, so one spelling is one domain.
ALTER TABLE project ADD COLUMN trusted_domains text[] NOT NULL DEFAULT '{}';

-- A CHECK cannot look inside an array on its own; this does, one element at a
-- time, and says yes to an empty list.
CREATE FUNCTION domain_list_well_formed(domains text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN coalesce((SELECT bool_and(d ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$') FROM unnest(domains) AS d), true);

ALTER TABLE project ADD CONSTRAINT project_trusted_domains_service CHECK (cardinality(trusted_domains) = 0 OR kind = 'service');
ALTER TABLE project ADD CONSTRAINT project_trusted_domains_shape CHECK (domain_list_well_formed(trusted_domains));

-- A session that came through an open door is refused when the desk names
-- domains and the address is at none of them, whatever the service believed.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION user_session_open_door() RETURNS trigger AS $$
BEGIN
    IF NEW.portal_project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM project
        WHERE id = NEW.portal_project_id AND kind = 'service' AND NOT portal_verifies AND archived_at IS NULL
    ) THEN
        RAISE EXCEPTION 'a session without a code belongs to a desk whose door is open'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.portal_project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM project p JOIN app_user u ON u.id = NEW.user_id
        WHERE p.id = NEW.portal_project_id
          AND (cardinality(p.trusted_domains) = 0
               OR lower(split_part(u.email::text, '@', 2)) = ANY (p.trusted_domains))
    ) THEN
        RAISE EXCEPTION 'a session without a code belongs to an address at a domain the desk trusts'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION user_session_open_door() RETURNS trigger AS $$
BEGIN
    IF NEW.portal_project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM project
        WHERE id = NEW.portal_project_id AND kind = 'service' AND NOT portal_verifies AND archived_at IS NULL
    ) THEN
        RAISE EXCEPTION 'a session without a code belongs to a desk whose door is open'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
ALTER TABLE project DROP CONSTRAINT IF EXISTS project_trusted_domains_shape;
ALTER TABLE project DROP CONSTRAINT IF EXISTS project_trusted_domains_service;
DROP FUNCTION IF EXISTS domain_list_well_formed(text[]);
ALTER TABLE project DROP COLUMN IF EXISTS trusted_domains;
