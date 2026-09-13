-- +goose Up
-- A desk decides whether its door asks for a code; only a desk may leave it
-- off, since only a desk has a door.
ALTER TABLE project ADD COLUMN portal_verifies boolean NOT NULL DEFAULT true;
ALTER TABLE project ADD CONSTRAINT project_portal_verifies_service CHECK (portal_verifies OR kind = 'service');

-- A session that came through an open door is the desk's: it sees that desk
-- and nothing else of the organization. Null is an ordinary session.
ALTER TABLE user_session ADD COLUMN portal_project_id uuid REFERENCES project(id) ON DELETE CASCADE;
CREATE INDEX user_session_portal_project_idx ON user_session (portal_project_id) WHERE portal_project_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION user_session_open_door() RETURNS trigger AS $$
BEGIN
    IF NEW.portal_project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM project
        WHERE id = NEW.portal_project_id AND kind = 'service' AND NOT portal_verifies AND archived_at IS NULL
    ) THEN
        RAISE EXCEPTION 'a session without a code belongs to a desk whose door is open'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER user_session_open_door
    BEFORE INSERT OR UPDATE OF portal_project_id ON user_session
    FOR EACH ROW EXECUTE FUNCTION user_session_open_door();

-- +goose Down
DROP TRIGGER IF EXISTS user_session_open_door ON user_session;
DROP FUNCTION IF EXISTS user_session_open_door();
DROP INDEX IF EXISTS user_session_portal_project_idx;
ALTER TABLE user_session DROP COLUMN IF EXISTS portal_project_id;
ALTER TABLE project DROP CONSTRAINT IF EXISTS project_portal_verifies_service;
ALTER TABLE project DROP COLUMN IF EXISTS portal_verifies;
