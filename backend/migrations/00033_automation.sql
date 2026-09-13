-- +goose Up
-- Rules: when something happens, if it looks like this, do these. A rule is
-- run by the worker from the event stream; a run is keyed by the rule and the
-- event, so a replayed event runs nothing twice. Every act of a rule is done
-- by the organization's automation account, an inactive member that never
-- signs in, so history and events name a real actor and a rule's own writes
-- can be told from a person's.

ALTER TABLE org ADD COLUMN automation_user_id uuid REFERENCES app_user(id) ON DELETE SET NULL;

-- +goose StatementBegin
DO $$
DECLARE
    o record;
    uid uuid;
BEGIN
    FOR o IN SELECT id FROM org WHERE automation_user_id IS NULL LOOP
        INSERT INTO app_user (email, name, is_active)
        VALUES ('automation+' || o.id || '@armature.invalid', 'Automation', false)
        RETURNING id INTO uid;
        INSERT INTO org_member (org_id, user_id, org_role) VALUES (o.id, uid, 'member');
        UPDATE org SET automation_user_id = uid WHERE id = o.id;
    END LOOP;
END;
$$;
-- +goose StatementEnd

CREATE TABLE automation_rule (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id           uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    -- Null means the rule is the organization's and watches every project.
    project_id       uuid REFERENCES project(id) ON DELETE CASCADE,
    name             text NOT NULL,
    enabled          boolean NOT NULL DEFAULT true,
    trigger          jsonb NOT NULL,
    conditions       jsonb NOT NULL DEFAULT '[]'::jsonb,
    actions          jsonb NOT NULL,
    allow_own_events boolean NOT NULL DEFAULT false,
    hourly_cap       integer NOT NULL DEFAULT 100,
    created_by       uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT automation_rule_name_shape CHECK (btrim(name) <> ''),
    CONSTRAINT automation_rule_has_actions CHECK (jsonb_typeof(actions) = 'array' AND jsonb_array_length(actions) > 0),
    CONSTRAINT automation_rule_conditions_list CHECK (jsonb_typeof(conditions) = 'array'),
    CONSTRAINT automation_rule_trigger_object CHECK (jsonb_typeof(trigger) = 'object'),
    CONSTRAINT automation_rule_cap_positive CHECK (hourly_cap > 0)
);

CREATE UNIQUE INDEX automation_rule_name_idx ON automation_rule (org_id, project_id, lower(name)) NULLS NOT DISTINCT;
CREATE INDEX automation_rule_org_idx ON automation_rule (org_id) WHERE enabled;

CREATE TRIGGER automation_rule_set_updated_at BEFORE UPDATE ON automation_rule
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A rule watches a project of its own organization, whatever the policy says
-- about the row itself.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION automation_rule_project_in_org() RETURNS trigger AS $$
BEGIN
    IF NEW.project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM project WHERE id = NEW.project_id AND org_id = NEW.org_id
    ) THEN
        RAISE EXCEPTION 'a rule can only watch a project of its own organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER automation_rule_project_in_org
    BEFORE INSERT OR UPDATE ON automation_rule
    FOR EACH ROW EXECUTE FUNCTION automation_rule_project_in_org();

CREATE TABLE automation_run (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    rule_id     uuid NOT NULL REFERENCES automation_rule(id) ON DELETE CASCADE,
    event_id    uuid NOT NULL,
    issue_id    uuid REFERENCES issue(id) ON DELETE SET NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    outcome     text NOT NULL DEFAULT 'running',
    reason      text NOT NULL DEFAULT '',
    actions     jsonb NOT NULL DEFAULT '[]'::jsonb,
    CONSTRAINT automation_run_outcome_known CHECK (outcome IN ('running', 'done', 'skipped', 'failed', 'capped')),
    CONSTRAINT automation_run_once_per_event UNIQUE (rule_id, event_id)
);

CREATE INDEX automation_run_rule_idx ON automation_run (rule_id, started_at DESC);

-- Endpoints: where the organization's events are posted, signed with a secret
-- the endpoint's owner was shown once. A delivery is one attempt at one
-- event; the next attempt is a new row.
CREATE TABLE webhook_endpoint (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name        text NOT NULL,
    url         text NOT NULL,
    secret      text NOT NULL,
    topics      text[] NOT NULL DEFAULT '{}',
    enabled     boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT webhook_endpoint_name_shape CHECK (btrim(name) <> ''),
    CONSTRAINT webhook_endpoint_url_shape CHECK (url ~ '^https?://')
);

CREATE UNIQUE INDEX webhook_endpoint_name_idx ON webhook_endpoint (org_id, lower(name));

CREATE TRIGGER webhook_endpoint_set_updated_at BEFORE UPDATE ON webhook_endpoint
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE webhook_delivery (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id          uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    endpoint_id     uuid NOT NULL REFERENCES webhook_endpoint(id) ON DELETE CASCADE,
    event_id        uuid NOT NULL,
    topic           text NOT NULL,
    attempt         integer NOT NULL DEFAULT 1,
    status          integer,
    error           text NOT NULL DEFAULT '',
    request_body    jsonb NOT NULL,
    next_attempt_at timestamptz,
    delivered_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT webhook_delivery_attempt_positive CHECK (attempt > 0),
    CONSTRAINT webhook_delivery_once UNIQUE (endpoint_id, event_id, attempt)
);

CREATE INDEX webhook_delivery_due_idx ON webhook_delivery (next_attempt_at) WHERE delivered_at IS NULL AND next_attempt_at IS NOT NULL;
CREATE INDEX webhook_delivery_endpoint_idx ON webhook_delivery (endpoint_id, created_at DESC);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['automation_rule', 'automation_run', 'webhook_endpoint', 'webhook_delivery']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON automation_rule, automation_run, webhook_endpoint, webhook_delivery TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS webhook_delivery CASCADE;
DROP TABLE IF EXISTS webhook_endpoint CASCADE;
DROP TABLE IF EXISTS automation_run CASCADE;
DROP TABLE IF EXISTS automation_rule CASCADE;
DROP FUNCTION IF EXISTS automation_rule_project_in_org();
ALTER TABLE org DROP COLUMN IF EXISTS automation_user_id;
