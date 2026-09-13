-- +goose Up
-- What a person is told about the work they are on: one row per person per
-- event per reason. The event id makes a second delivery of the same event a
-- no-op, so at-least-once from the stream is exactly-once in the inbox. Mail
-- is a copy of a row here, never the record itself.

CREATE TABLE notification (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    issue_id   uuid REFERENCES issue(id) ON DELETE CASCADE,
    kind       text NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    link       text NOT NULL DEFAULT '',
    event_id   uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at    timestamptz,
    CONSTRAINT notification_kind_known CHECK (kind IN
        ('assigned', 'mentioned', 'commented', 'transitioned', 'watching', 'sla_breached', 'rule', 'filter')),
    CONSTRAINT notification_once_per_event UNIQUE (user_id, event_id, kind)
);

CREATE INDEX notification_inbox_idx  ON notification (user_id, created_at DESC);
CREATE INDEX notification_unread_idx ON notification (user_id) WHERE read_at IS NULL;

-- How each person wants to hear: which kinds by mail, which in the inbox, and
-- whether mail comes at once or bundled. An absent row means everything, at once.
CREATE TABLE notification_preference (
    org_id    uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    mail      jsonb NOT NULL DEFAULT '{}'::jsonb,
    inapp     jsonb NOT NULL DEFAULT '{}'::jsonb,
    digest    text NOT NULL DEFAULT 'off',
    watch_own boolean NOT NULL DEFAULT true,
    PRIMARY KEY (org_id, user_id),
    CONSTRAINT notification_preference_objects CHECK (jsonb_typeof(mail) = 'object' AND jsonb_typeof(inapp) = 'object'),
    CONSTRAINT notification_preference_digest_known CHECK (digest IN ('off', 'hourly', 'daily'))
);

-- Rows waiting to be bundled into one mail.
CREATE TABLE notification_digest (
    org_id          uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    notification_id uuid NOT NULL REFERENCES notification(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, notification_id)
);

-- A notification about an issue belongs to the issue's organization; the
-- policy checks the row's own org, this checks the two agree.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notification_same_org() RETURNS trigger AS $$
BEGIN
    IF NEW.issue_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM issue WHERE id = NEW.issue_id AND org_id = NEW.org_id
    ) THEN
        RAISE EXCEPTION 'a notification must belong to the same organization as its issue'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER notification_same_org
    BEFORE INSERT OR UPDATE ON notification
    FOR EACH ROW EXECUTE FUNCTION notification_same_org();

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['notification', 'notification_preference', 'notification_digest']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON notification, notification_preference, notification_digest TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS notification_digest CASCADE;
DROP TABLE IF EXISTS notification_preference CASCADE;
DROP TABLE IF EXISTS notification CASCADE;
DROP FUNCTION IF EXISTS notification_same_org();
