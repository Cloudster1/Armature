-- +goose Up
-- Foundation: organizations, users, membership, sessions, tokens and the row
-- level security machinery that every later tenant table plugs into.

CREATE EXTENSION IF NOT EXISTS citext;

-- +goose StatementBegin
-- current_org_id reads the tenant that the surrounding transaction is scoped to.
-- It returns NULL when unset, which makes every RLS policy fail closed: an
-- un-scoped connection sees no tenant rows at all rather than all of them.
CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.org_id', true), '')::uuid
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------- tenants ---

CREATE TABLE org (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    slug        citext NOT NULL UNIQUE,
    name        text NOT NULL,
    settings    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT org_slug_shape CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$')
);

CREATE TRIGGER org_set_updated_at BEFORE UPDATE ON org
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ------------------------------------------------------------------ users ---

-- A person is global, not per tenant: one account can belong to several
-- organizations, which is why app_user carries no org_id and no RLS policy.
CREATE TABLE app_user (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    email         citext NOT NULL UNIQUE,
    name          text NOT NULL,
    password_hash text,
    avatar_url    text,
    timezone      text NOT NULL DEFAULT 'UTC',
    locale        text NOT NULL DEFAULT 'en',
    is_active     boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT app_user_email_shape CHECK (position('@' in email) > 1)
);

CREATE TRIGGER app_user_set_updated_at BEFORE UPDATE ON app_user
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- org_role separates people who consume a licensed seat from portal customers,
-- who may only ever see their own service requests.
CREATE TABLE org_member (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    org_role   text NOT NULL CHECK (org_role IN ('owner', 'admin', 'member', 'customer')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

CREATE INDEX org_member_user_idx ON org_member (user_id);

CREATE TRIGGER org_member_set_updated_at BEFORE UPDATE ON org_member
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- --------------------------------------------------------------- sessions ---

-- Sessions span organizations because a user switches tenant without logging in
-- again; current_org_id records which tenant the session is currently acting in.
CREATE TABLE user_session (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id        uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    token_hash     bytea NOT NULL UNIQUE,
    current_org_id uuid REFERENCES org(id) ON DELETE SET NULL,
    user_agent     text,
    ip             inet,
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_seen_at   timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL
);

CREATE INDEX user_session_user_idx ON user_session (user_id);
CREATE INDEX user_session_expiry_idx ON user_session (expires_at);

CREATE TABLE api_token (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name         text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    scopes       text[] NOT NULL DEFAULT '{}',
    last_used_at timestamptz,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX api_token_org_user_idx ON api_token (org_id, user_id);

CREATE TABLE org_invite (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    email      citext NOT NULL,
    org_role   text NOT NULL CHECK (org_role IN ('admin', 'member', 'customer')),
    token_hash bytea NOT NULL UNIQUE,
    invited_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX org_invite_pending_idx ON org_invite (org_id, email)
    WHERE accepted_at IS NULL;

-- ------------------------------------------------------- audit and events ---

CREATE TABLE audit_log (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    actor_user_id uuid REFERENCES app_user(id) ON DELETE SET NULL,
    action        text NOT NULL,
    target_type   text NOT NULL,
    target_id     uuid,
    data          jsonb NOT NULL DEFAULT '{}'::jsonb,
    ip            inet,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_org_created_idx ON audit_log (org_id, created_at DESC);
CREATE INDEX audit_log_target_idx ON audit_log (org_id, target_type, target_id);

-- The transactional outbox: domain events are written in the same transaction
-- as the change that produced them, then relayed to the worker. That is what
-- makes "issue transitioned" and "notify the watchers" impossible to disagree.
CREATE TABLE outbox_event (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    topic        text NOT NULL,
    payload      jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    attempts     integer NOT NULL DEFAULT 0,
    last_error   text
);

-- Partial index: the relay only ever scans the unpublished tail, so the index
-- stays small no matter how much history accumulates.
CREATE INDEX outbox_event_unpublished_idx ON outbox_event (created_at)
    WHERE published_at IS NULL;

-- ---------------------------------------------------- row level security ----

-- FORCE, not just ENABLE: without it the table owner bypasses its own policies,
-- and migrations plus tests routinely connect as the owner.
ALTER TABLE org         ENABLE ROW LEVEL SECURITY;
ALTER TABLE org         FORCE  ROW LEVEL SECURITY;
ALTER TABLE org_member  ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_member  FORCE  ROW LEVEL SECURITY;
ALTER TABLE api_token   ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_token   FORCE  ROW LEVEL SECURITY;
ALTER TABLE org_invite  ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_invite  FORCE  ROW LEVEL SECURITY;
ALTER TABLE audit_log   ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log   FORCE  ROW LEVEL SECURITY;
ALTER TABLE outbox_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_event FORCE  ROW LEVEL SECURITY;

CREATE POLICY org_tenant_isolation ON org
    USING (id = current_org_id())
    WITH CHECK (id = current_org_id());

CREATE POLICY org_member_tenant_isolation ON org_member
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

CREATE POLICY api_token_tenant_isolation ON api_token
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

CREATE POLICY org_invite_tenant_isolation ON org_invite
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

CREATE POLICY audit_log_tenant_isolation ON audit_log
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

CREATE POLICY outbox_event_tenant_isolation ON outbox_event
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

-- Signup, login and the outbox relay legitimately operate before or across
-- tenants. They run as armature_admin, which is exempt by policy rather than by
-- being a superuser, so the exemption is visible in the schema.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY org_admin_bypass ON org TO armature_admin USING (true) WITH CHECK (true);
        CREATE POLICY org_member_admin_bypass ON org_member TO armature_admin USING (true) WITH CHECK (true);
        CREATE POLICY api_token_admin_bypass ON api_token TO armature_admin USING (true) WITH CHECK (true);
        CREATE POLICY org_invite_admin_bypass ON org_invite TO armature_admin USING (true) WITH CHECK (true);
        CREATE POLICY audit_log_admin_bypass ON audit_log TO armature_admin USING (true) WITH CHECK (true);
        CREATE POLICY outbox_event_admin_bypass ON outbox_event TO armature_admin USING (true) WITH CHECK (true);
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT USAGE ON SCHEMA public TO armature_app, armature_admin;
        GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO armature_app, armature_admin;
        GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS outbox_event, audit_log, org_invite, api_token, user_session, org_member, app_user, org CASCADE;
DROP FUNCTION IF EXISTS set_updated_at();
DROP FUNCTION IF EXISTS current_org_id();
