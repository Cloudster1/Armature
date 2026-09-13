-- +goose Up
-- Sprints: a named stretch of time a team commits work to, and the capacity
-- that says how much work that is.

CREATE TYPE sprint_state AS ENUM ('future', 'active', 'closed');

CREATE TABLE sprint (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id   uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name         text NOT NULL,
    goal         text NOT NULL DEFAULT '',
    state        sprint_state NOT NULL DEFAULT 'future',
    -- Dates are optional while a sprint is still being sketched; starting one
    -- without them is refused, because a sprint with no end never ends.
    starts_on    date,
    ends_on      date,
    -- Capacity is in the same unit as an issue's estimate. Null means the team
    -- has not said, which reads differently from a capacity of zero.
    capacity     numeric(8,2),
    -- Position orders the backlog of future sprints, which is the order a team
    -- plans them in and has nothing to do with their dates.
    position     integer NOT NULL DEFAULT 0,
    started_at   timestamptz,
    completed_at timestamptz,
    -- What the sprint turned out to be, written once when it is completed. A
    -- report computed live would change every time somebody re-estimated an
    -- issue months later, which would make velocity meaningless.
    committed    numeric(10,2),
    completed    numeric(10,2),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sprint_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT sprint_range_not_backwards
        CHECK (starts_on IS NULL OR ends_on IS NULL OR starts_on <= ends_on),
    CONSTRAINT sprint_capacity_not_negative CHECK (capacity IS NULL OR capacity >= 0)
);

CREATE INDEX sprint_project_idx ON sprint (project_id, state, position);

-- A team runs one sprint at a time. Two would make "the sprint" ambiguous in
-- every sentence anybody says about the work.
CREATE UNIQUE INDEX sprint_one_active_idx ON sprint (project_id) WHERE state = 'active';

CREATE TRIGGER sprint_set_updated_at BEFORE UPDATE ON sprint
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION sprint_state_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.state = 'active' AND (NEW.starts_on IS NULL OR NEW.ends_on IS NULL) THEN
        RAISE EXCEPTION 'a running sprint needs a start and an end'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.state = 'active' AND NEW.started_at IS NULL THEN
        NEW.started_at := now();
    END IF;
    IF NEW.state = 'closed' AND NEW.completed_at IS NULL THEN
        NEW.completed_at := now();
    END IF;
    -- A sprint only moves forwards. Reopening a closed sprint would make its
    -- report a lie, and the work it left behind has already moved on.
    IF TG_OP = 'UPDATE' AND OLD.state = 'closed' AND NEW.state <> 'closed' THEN
        RAISE EXCEPTION 'a completed sprint cannot be reopened'
            USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.state = 'active' AND NEW.state = 'future' THEN
        RAISE EXCEPTION 'a running sprint cannot go back to being planned'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER sprint_state_check
    BEFORE INSERT OR UPDATE ON sprint
    FOR EACH ROW EXECUTE FUNCTION sprint_state_guard();

-- ------------------------------------------------------ issues in sprints ---

-- Null is the backlog: work that exists but nobody has committed to yet.
ALTER TABLE issue ADD COLUMN sprint_id uuid REFERENCES sprint(id) ON DELETE SET NULL;

-- An estimate is how much work this is, in whatever unit the team counts in.
-- Null is unestimated, which is not the same as zero and is counted separately.
ALTER TABLE issue ADD COLUMN estimate numeric(6,2);
ALTER TABLE issue ADD CONSTRAINT issue_estimate_not_negative
    CHECK (estimate IS NULL OR estimate >= 0);

CREATE INDEX issue_sprint_idx ON issue (sprint_id) WHERE sprint_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_sprint_guard() RETURNS trigger AS $$
DECLARE
    sprint_project uuid;
    sprint_org     uuid;
    joined_state   sprint_state;
BEGIN
    IF NEW.sprint_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id, org_id, state INTO sprint_project, sprint_org, joined_state
      FROM sprint WHERE id = NEW.sprint_id;

    IF sprint_org IS DISTINCT FROM NEW.org_id OR sprint_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'an issue can only join a sprint in its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    -- Work cannot be added to a sprint that is already over; the report on it
    -- has been written.
    IF joined_state = 'closed' AND (TG_OP = 'INSERT' OR OLD.sprint_id IS DISTINCT FROM NEW.sprint_id) THEN
        RAISE EXCEPTION 'that sprint is over'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_sprint_check
    BEFORE INSERT OR UPDATE OF sprint_id, project_id, org_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_sprint_guard();

-- ---------------------------------------------------- row level security ----

ALTER TABLE sprint ENABLE ROW LEVEL SECURITY;
ALTER TABLE sprint FORCE  ROW LEVEL SECURITY;

CREATE POLICY sprint_tenant_isolation ON sprint
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY sprint_admin_bypass ON sprint TO armature_admin USING (true) WITH CHECK (true);
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON sprint TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS issue_sprint_check ON issue;
DROP FUNCTION IF EXISTS issue_sprint_guard();
DROP INDEX IF EXISTS issue_sprint_idx;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_estimate_not_negative;
ALTER TABLE issue DROP COLUMN IF EXISTS estimate;
ALTER TABLE issue DROP COLUMN IF EXISTS sprint_id;
DROP TRIGGER IF EXISTS sprint_state_check ON sprint;
DROP FUNCTION IF EXISTS sprint_state_guard();
DROP TABLE IF EXISTS sprint CASCADE;
DROP TYPE IF EXISTS sprint_state;
