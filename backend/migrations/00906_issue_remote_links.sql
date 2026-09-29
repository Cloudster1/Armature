-- +goose Up
-- A page elsewhere about an issue, kept in step by the application holding it:
-- one row per address, so a second sync retitles rather than repeats.
CREATE TABLE issue_remote_link (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id   uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    url        text NOT NULL,
    title      text NOT NULL,
    source     text NOT NULL,
    icon_url   text,
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- The same bounds the service checks, so a row written any other way is
    -- held to them too.
    CONSTRAINT issue_remote_link_url_shape CHECK (url ~* '^https?://[^\s/]+' AND char_length(url) <= 2048),
    CONSTRAINT issue_remote_link_icon_shape CHECK (icon_url IS NULL OR (icon_url ~* '^https?://[^\s/]+' AND char_length(icon_url) <= 2048)),
    CONSTRAINT issue_remote_link_title_not_blank CHECK (btrim(title) <> '' AND char_length(title) <= 255),
    CONSTRAINT issue_remote_link_source_not_blank CHECK (btrim(source) <> '' AND char_length(source) <= 100)
);

CREATE UNIQUE INDEX issue_remote_link_url_idx ON issue_remote_link (issue_id, url);

CREATE TRIGGER issue_remote_link_set_updated_at BEFORE UPDATE ON issue_remote_link
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The policy checks the row's own organization; this checks the issue is in it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_remote_link_same_org() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM issue WHERE id = NEW.issue_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'a page link must belong to the same organization as its issue'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_remote_link_same_org
    BEFORE INSERT OR UPDATE ON issue_remote_link
    FOR EACH ROW EXECUTE FUNCTION issue_remote_link_same_org();

ALTER TABLE issue_remote_link ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_remote_link FORCE ROW LEVEL SECURITY;
CREATE POLICY issue_remote_link_tenant_isolation ON issue_remote_link
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY issue_remote_link_admin_bypass ON issue_remote_link
            TO armature_admin USING (true) WITH CHECK (true);
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON issue_remote_link TO armature_app, armature_admin;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS issue_remote_link;
DROP FUNCTION IF EXISTS issue_remote_link_same_org();
