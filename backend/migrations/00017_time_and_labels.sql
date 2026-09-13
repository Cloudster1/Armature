-- +goose Up
-- Time on an issue: an estimate of how long it will take, how long is thought
-- to remain, and the work logged against it. Minutes, because a quarter of an
-- hour is the smallest thing anybody honestly logs and floating point hours
-- invite 0.1666.

ALTER TABLE issue ADD COLUMN time_estimate_minutes integer;
ALTER TABLE issue ADD COLUMN time_remaining_minutes integer;
ALTER TABLE issue ADD CONSTRAINT issue_time_estimate_not_negative
    CHECK (time_estimate_minutes IS NULL OR time_estimate_minutes >= 0);
ALTER TABLE issue ADD CONSTRAINT issue_time_remaining_not_negative
    CHECK (time_remaining_minutes IS NULL OR time_remaining_minutes >= 0);

-- A worklog is one stretch of somebody's time on one issue. Time spent is the
-- sum of these, never a column of its own, so it cannot drift from them.
CREATE TABLE issue_worklog (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id   uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    author_id  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    minutes    integer NOT NULL,
    started_on date NOT NULL DEFAULT CURRENT_DATE,
    note       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_worklog_minutes_positive CHECK (minutes > 0)
);

CREATE INDEX issue_worklog_issue_idx ON issue_worklog (issue_id, started_on, created_at);
CREATE INDEX issue_worklog_author_idx ON issue_worklog (author_id, started_on);

CREATE TRIGGER issue_worklog_set_updated_at BEFORE UPDATE ON issue_worklog
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Labels belong to the organization, so the same word means the same thing in
-- every project, and an issue carries any number of them.
CREATE TABLE label (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name       text NOT NULL,
    color      text NOT NULL DEFAULT 'gray',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT label_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT label_name_has_no_spaces CHECK (name !~ '\s')
);

CREATE UNIQUE INDEX label_org_name_idx ON label (org_id, lower(name));

CREATE TRIGGER label_set_updated_at BEFORE UPDATE ON label
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE issue_label (
    org_id   uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    label_id uuid NOT NULL REFERENCES label(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, label_id)
);

CREATE INDEX issue_label_label_idx ON issue_label (label_id);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['issue_worklog', 'label', 'issue_label']
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
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON issue_worklog, label, issue_label TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS issue_label, label, issue_worklog CASCADE;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_time_remaining_not_negative;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_time_estimate_not_negative;
ALTER TABLE issue DROP COLUMN IF EXISTS time_remaining_minutes;
ALTER TABLE issue DROP COLUMN IF EXISTS time_estimate_minutes;
