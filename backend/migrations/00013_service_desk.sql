-- +goose Up
-- The service desk: what customers raise, how agents answer, and how long it
-- takes.

-- ------------------------------------------------------- request types ----

-- A request type is what a customer chooses from: a name in their words, that
-- becomes an issue of a type in ours.
CREATE TABLE request_type (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id    uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name          text NOT NULL,
    description   text NOT NULL DEFAULT '',
    issue_type_id uuid NOT NULL REFERENCES issue_type(id) ON DELETE RESTRICT,
    priority      issue_priority NOT NULL DEFAULT 'medium',
    position      integer NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT request_type_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX request_type_project_name_idx ON request_type (project_id, lower(name));
CREATE INDEX request_type_project_idx ON request_type (project_id, position);

CREATE TRIGGER request_type_set_updated_at BEFORE UPDATE ON request_type
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- An issue raised through the portal remembers which request type it came in
-- as. Null is an issue an agent made, which is most of them.
ALTER TABLE issue ADD COLUMN request_type_id uuid REFERENCES request_type(id) ON DELETE SET NULL;
CREATE INDEX issue_request_type_idx ON issue (request_type_id) WHERE request_type_id IS NOT NULL;

-- An internal note is a comment the customer does not see. It is a flag on the
-- comment rather than a second table, because it is the same conversation
-- read by two audiences.
ALTER TABLE issue_comment ADD COLUMN is_internal boolean NOT NULL DEFAULT false;

-- ----------------------------------------------------------------- slas ----

-- What is being measured: how long until somebody answers, or until it is
-- resolved.
CREATE TYPE sla_metric AS ENUM ('first_response', 'resolution');

-- A policy is a goal per priority, in minutes, for one metric in one project.
-- The goals are one JSON object keyed by priority, so adding a priority is not
-- a schema change. Statuses in pause_status_ids stop the clock, which is how
-- "waiting for the customer" is not counted against the team.
CREATE TABLE sla_policy (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id           uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id       uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name             text NOT NULL,
    metric           sla_metric NOT NULL,
    goals            jsonb NOT NULL DEFAULT '{}'::jsonb,
    pause_status_ids uuid[] NOT NULL DEFAULT '{}',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sla_policy_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX sla_policy_project_metric_idx ON sla_policy (project_id, metric);

CREATE TRIGGER sla_policy_set_updated_at BEFORE UPDATE ON sla_policy
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A timer is one policy applied to one issue. Time counts while running_since
-- is set; a pause banks what has run so far into elapsed_seconds and clears it.
-- Completed and breached are recorded once and kept, so a report can say what
-- happened without recomputing it against a policy that may have changed.
CREATE TABLE sla_timer (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id          uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id        uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    policy_id       uuid NOT NULL REFERENCES sla_policy(id) ON DELETE CASCADE,
    goal_minutes    integer NOT NULL CHECK (goal_minutes > 0),
    started_at      timestamptz NOT NULL DEFAULT now(),
    elapsed_seconds bigint NOT NULL DEFAULT 0 CHECK (elapsed_seconds >= 0),
    running_since   timestamptz,
    completed_at    timestamptz,
    breached_at     timestamptz,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issue_id, policy_id)
);

CREATE INDEX sla_timer_running_idx ON sla_timer (running_since) WHERE running_since IS NOT NULL AND completed_at IS NULL;

CREATE TRIGGER sla_timer_set_updated_at BEFORE UPDATE ON sla_timer
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['request_type', 'sla_policy', 'sla_timer']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id())',
            t || '_tenant_isolation', t);

        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
            EXECUTE format(
                'CREATE POLICY %I ON %I TO armature_admin USING (true) WITH CHECK (true)',
                t || '_admin_bypass', t);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON request_type, sla_policy, sla_timer TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS sla_timer, sla_policy CASCADE;
DROP TYPE IF EXISTS sla_metric;
ALTER TABLE issue_comment DROP COLUMN IF EXISTS is_internal;
DROP INDEX IF EXISTS issue_request_type_idx;
ALTER TABLE issue DROP COLUMN IF EXISTS request_type_id;
DROP TABLE IF EXISTS request_type CASCADE;
