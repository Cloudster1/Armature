-- +goose Up
-- Every key an issue has left by moving, one row each: a single column kept
-- only the last, so the address mailed around before stopped finding it.
CREATE TABLE issue_old_key (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id   uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    key        text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- A key names one issue in an organization, an old one as much as a live one.
    CONSTRAINT issue_old_key_pkey PRIMARY KEY (org_id, key),
    CONSTRAINT issue_old_key_shape CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}-[0-9]+$')
);

CREATE INDEX issue_old_key_issue_idx ON issue_old_key (issue_id);

-- The policy checks the row's own organization; this checks the issue is in it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_old_key_same_org() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM issue WHERE id = NEW.issue_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'an old key must belong to the same organization as its issue'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_old_key_same_org
    BEFORE INSERT OR UPDATE ON issue_old_key
    FOR EACH ROW EXECUTE FUNCTION issue_old_key_same_org();

-- The one key each moved issue kept so far. The issue table hides its rows
-- from a migration run as a plain owner, so its row security is lifted for
-- the copy; a key two issues kept goes to the one that left it last.
ALTER TABLE issue NO FORCE ROW LEVEL SECURITY;
INSERT INTO issue_old_key (org_id, issue_id, key, created_at)
SELECT DISTINCT ON (org_id, moved_from) org_id, id, moved_from, updated_at
  FROM issue
 WHERE moved_from IS NOT NULL
 ORDER BY org_id, moved_from, updated_at DESC, id;
ALTER TABLE issue FORCE ROW LEVEL SECURITY;

DROP INDEX IF EXISTS issue_moved_from_idx;
ALTER TABLE issue DROP COLUMN moved_from;

ALTER TABLE issue_old_key ENABLE ROW LEVEL SECURITY;
ALTER TABLE issue_old_key FORCE ROW LEVEL SECURITY;
CREATE POLICY issue_old_key_tenant_isolation ON issue_old_key
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY issue_old_key_admin_bypass ON issue_old_key
            TO armature_admin USING (true) WITH CHECK (true);
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON issue_old_key TO armature_app, armature_admin;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- The column holds one key, so each issue gets back the last it left.
ALTER TABLE issue ADD COLUMN moved_from text;
ALTER TABLE issue NO FORCE ROW LEVEL SECURITY;
ALTER TABLE issue_old_key NO FORCE ROW LEVEL SECURITY;
UPDATE issue i
   SET moved_from = (SELECT k.key FROM issue_old_key k WHERE k.issue_id = i.id
                      ORDER BY k.created_at DESC, k.key DESC LIMIT 1)
 WHERE EXISTS (SELECT 1 FROM issue_old_key k WHERE k.issue_id = i.id);
ALTER TABLE issue FORCE ROW LEVEL SECURITY;
CREATE INDEX issue_moved_from_idx ON issue (org_id, moved_from) WHERE moved_from IS NOT NULL;
DROP TABLE IF EXISTS issue_old_key;
DROP FUNCTION IF EXISTS issue_old_key_same_org();
