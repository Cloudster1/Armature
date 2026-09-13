-- +goose Up
-- Milestones: a named point a project is working towards, with the issues
-- that have to be finished for it to count as reached. Progress is never
-- stored; it is read off the assigned issues' statuses, so it cannot drift.

CREATE TABLE milestone (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    -- Optional while the milestone is an intention rather than a date.
    due_on      date,
    -- Set when the milestone is declared reached or abandoned. Work can no
    -- longer be assigned to it, but what was assigned stays for the record.
    closed_at   timestamptz,
    position    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT milestone_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX milestone_project_name_idx ON milestone (project_id, lower(name));
CREATE INDEX milestone_project_idx ON milestone (project_id, closed_at, due_on);

CREATE TRIGGER milestone_set_updated_at BEFORE UPDATE ON milestone
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE issue ADD COLUMN milestone_id uuid REFERENCES milestone(id) ON DELETE SET NULL;
CREATE INDEX issue_milestone_idx ON issue (milestone_id) WHERE milestone_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_milestone_guard() RETURNS trigger AS $$
DECLARE
    milestone_project uuid;
    milestone_org     uuid;
    milestone_closed  timestamptz;
BEGIN
    IF NEW.milestone_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id, org_id, closed_at INTO milestone_project, milestone_org, milestone_closed
      FROM milestone WHERE id = NEW.milestone_id;

    IF milestone_org IS DISTINCT FROM NEW.org_id OR milestone_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'an issue can only be assigned to a milestone in its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF milestone_closed IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.milestone_id IS DISTINCT FROM NEW.milestone_id) THEN
        RAISE EXCEPTION 'that milestone is closed'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_milestone_check
    BEFORE INSERT OR UPDATE OF milestone_id, project_id, org_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_milestone_guard();

-- ---------------------------------------------------- row level security ----

ALTER TABLE milestone ENABLE ROW LEVEL SECURITY;
ALTER TABLE milestone FORCE  ROW LEVEL SECURITY;

CREATE POLICY milestone_tenant_isolation ON milestone
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY milestone_admin_bypass ON milestone TO armature_admin USING (true) WITH CHECK (true);
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON milestone TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS issue_milestone_check ON issue;
DROP FUNCTION IF EXISTS issue_milestone_guard();
DROP INDEX IF EXISTS issue_milestone_idx;
ALTER TABLE issue DROP COLUMN IF EXISTS milestone_id;
DROP TABLE IF EXISTS milestone CASCADE;
