-- +goose Up
-- A saved filter is a query with a name. Everything that uses one reads the
-- query live, so editing it changes every view at once and nothing stores a
-- stale copy. Subscriptions mail its result on a schedule. Moving an issue
-- keeps its old key on the row, so the address it had still finds it, and
-- an import keeps what it did as a report.

CREATE TABLE saved_filter (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    owner_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name       text NOT NULL,
    query      text NOT NULL,
    shared     boolean NOT NULL DEFAULT false,
    columns    text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT saved_filter_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT saved_filter_query_not_blank CHECK (btrim(query) <> '')
);

CREATE UNIQUE INDEX saved_filter_owner_name_idx ON saved_filter (owner_id, lower(name));
CREATE INDEX saved_filter_org_idx ON saved_filter (org_id, shared);

CREATE TRIGGER saved_filter_set_updated_at BEFORE UPDATE ON saved_filter
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A customer's saved search stays theirs: sharing is for the people in the organization.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION saved_filter_share_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.owner_id AND org_role = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a saved filter'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER saved_filter_share_check
    BEFORE INSERT OR UPDATE ON saved_filter
    FOR EACH ROW EXECUTE FUNCTION saved_filter_share_guard();

CREATE TABLE saved_filter_star (
    org_id    uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    filter_id uuid NOT NULL REFERENCES saved_filter(id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    PRIMARY KEY (filter_id, user_id)
);

CREATE TABLE saved_filter_subscription (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    filter_id    uuid NOT NULL REFERENCES saved_filter(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    schedule     text NOT NULL,
    hour         integer NOT NULL DEFAULT 8,
    weekday      integer,
    last_sent_at timestamptz,
    CONSTRAINT saved_filter_subscription_schedule_known CHECK (schedule IN ('daily', 'weekly')),
    CONSTRAINT saved_filter_subscription_hour_range CHECK (hour BETWEEN 0 AND 23),
    CONSTRAINT saved_filter_subscription_weekday_range CHECK (weekday IS NULL OR weekday BETWEEN 0 AND 6),
    CONSTRAINT saved_filter_subscription_once UNIQUE (filter_id, user_id)
);

-- The key an issue had before it was moved, so its old address still finds it.
ALTER TABLE issue ADD COLUMN moved_from text;
CREATE INDEX issue_moved_from_idx ON issue (org_id, moved_from) WHERE moved_from IS NOT NULL;

CREATE TABLE import_job (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    actor_id   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    filename   text NOT NULL DEFAULT '',
    mapping    jsonb NOT NULL DEFAULT '{}'::jsonb,
    dry_run    boolean NOT NULL,
    report     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX import_job_project_idx ON import_job (project_id, created_at DESC);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['saved_filter', 'saved_filter_star', 'saved_filter_subscription', 'import_job']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON saved_filter, saved_filter_star, saved_filter_subscription, import_job TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS import_job CASCADE;
DROP INDEX IF EXISTS issue_moved_from_idx;
ALTER TABLE issue DROP COLUMN IF EXISTS moved_from;
DROP TABLE IF EXISTS saved_filter_subscription CASCADE;
DROP TABLE IF EXISTS saved_filter_star CASCADE;
DROP TABLE IF EXISTS saved_filter CASCADE;
DROP FUNCTION IF EXISTS saved_filter_share_guard();
