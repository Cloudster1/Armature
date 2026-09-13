-- +goose Up
-- A sprint's progress, one row per day: what was in it, what was done. The
-- worker writes today's row on a timer and the day's last write stands, so a
-- burndown can be drawn long after the issues have moved on. Completion also
-- keeps how many issues it finished and carried, which it only ever reported.

CREATE TABLE sprint_snapshot (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    sprint_id   uuid NOT NULL REFERENCES sprint(id) ON DELETE CASCADE,
    day         date NOT NULL,
    -- Points committed and points done, counted the way the plan counts them.
    scope       numeric(10,2) NOT NULL,
    done        numeric(10,2) NOT NULL,
    remaining   numeric(10,2) GENERATED ALWAYS AS (scope - done) STORED,
    issues      integer NOT NULL,
    issues_done integer NOT NULL,
    unestimated integer NOT NULL,
    taken_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sprint_snapshot_not_negative
        CHECK (scope >= 0 AND done >= 0 AND issues >= 0 AND issues_done >= 0 AND unestimated >= 0)
);

CREATE UNIQUE INDEX sprint_snapshot_day_idx ON sprint_snapshot (sprint_id, day);

ALTER TABLE sprint
    ADD COLUMN finished integer,
    ADD COLUMN carried  integer;

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['sprint_snapshot']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON sprint_snapshot TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS sprint_snapshot CASCADE;
ALTER TABLE sprint DROP COLUMN IF EXISTS finished, DROP COLUMN IF EXISTS carried;
