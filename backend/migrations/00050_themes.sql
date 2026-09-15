-- +goose Up
-- A theme is a saved redefinition of the interface's tokens, with the files
-- it draws from. It is its maker's, shared with the organization when they
-- say so, and chosen per person; deleting it returns its people to the
-- built-in theme by the cascade.
CREATE TABLE theme (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    owner_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    name       text NOT NULL,
    shared     boolean NOT NULL DEFAULT false,
    spec       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT theme_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX theme_owner_name_idx ON theme (owner_id, lower(name));
CREATE INDEX theme_org_idx ON theme (org_id, shared);

CREATE TRIGGER theme_set_updated_at BEFORE UPDATE ON theme
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A customer's theme stays theirs: sharing is for the people in the organization.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION theme_share_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.owner_id AND org_role = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a theme'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER theme_share_check
    BEFORE INSERT OR UPDATE ON theme
    FOR EACH ROW EXECUTE FUNCTION theme_share_guard();

-- The files a theme draws from. The bytes live in the object store under
-- theme/<theme>/<asset>; the row is what says the file exists.
CREATE TABLE theme_asset (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    theme_id     uuid NOT NULL REFERENCES theme(id) ON DELETE CASCADE,
    name         text NOT NULL,
    content_type text NOT NULL,
    size         bigint NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX theme_asset_theme_idx ON theme_asset (theme_id);

-- Who chose what. One row per person per organization.
CREATE TABLE user_theme (
    org_id   uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    theme_id uuid NOT NULL REFERENCES theme(id) ON DELETE CASCADE,
    PRIMARY KEY (org_id, user_id)
);

CREATE INDEX user_theme_theme_idx ON user_theme (theme_id);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['theme', 'theme_asset', 'user_theme'] LOOP
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON theme, theme_asset, user_theme TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS user_theme;
DROP TABLE IF EXISTS theme_asset;
DROP TABLE IF EXISTS theme;
DROP FUNCTION IF EXISTS theme_share_guard();
