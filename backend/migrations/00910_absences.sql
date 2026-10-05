-- +goose Up
-- The days somebody is away. There is deliberately no reason and no note: the
-- plan needs to know that a person is away, and nobody needs to know why.

-- Lets an exclusion constraint compare the uuids beside the date range. It is
-- a trusted extension, so the database's owner may create it without being a superuser.
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE absence (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    -- Both days are included: one day away starts and ends on the same day.
    starts_on  date NOT NULL,
    ends_on    date NOT NULL,
    -- Half of the one day away; the person works the other half.
    half_day   boolean NOT NULL DEFAULT false,
    -- Who wrote it down, which is not always the person away.
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT absence_in_order CHECK (ends_on >= starts_on),
    CONSTRAINT absence_half_day_is_one_day CHECK (NOT half_day OR starts_on = ends_on),
    -- The same bound the service checks: MaxAbsenceDays, both ends counted.
    CONSTRAINT absence_span CHECK (ends_on - starts_on < 366),
    -- One person is away on a day once; a second absence over it is refused.
    CONSTRAINT absence_no_overlap EXCLUDE USING gist (
        org_id WITH =, user_id WITH =, daterange(starts_on, ends_on, '[]') WITH &&
    )
);

CREATE INDEX absence_days_idx ON absence (org_id, ends_on, starts_on);

CREATE TRIGGER absence_set_updated_at BEFORE UPDATE ON absence
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Only somebody who works here is away from it: a portal customer is not.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION absence_guard() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM org_member
         WHERE org_id = NEW.org_id AND user_id = NEW.user_id
           AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member of the organization is away from it'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER absence_check
    BEFORE INSERT OR UPDATE ON absence
    FOR EACH ROW EXECUTE FUNCTION absence_guard();

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE absence ENABLE ROW LEVEL SECURITY;
    ALTER TABLE absence FORCE ROW LEVEL SECURITY;
    CREATE POLICY absence_tenant_isolation ON absence
        USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY absence_admin_bypass ON absence TO armature_admin USING (true) WITH CHECK (true);
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON absence TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS absence CASCADE;
DROP FUNCTION IF EXISTS absence_guard();
DROP EXTENSION IF EXISTS btree_gist;
