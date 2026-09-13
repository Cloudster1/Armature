-- +goose Up
-- A session remembers how it was proven, because what it may reach follows
-- from that: a password reaches every organization, a mailed code only one.
ALTER TABLE user_session ADD COLUMN proof text NOT NULL DEFAULT 'password'
    CONSTRAINT user_session_proof_known CHECK (proof IN ('password', 'invite', 'portal_code', 'open_door', 'oidc'));
UPDATE user_session SET proof = 'open_door' WHERE portal_project_id IS NOT NULL;
ALTER TABLE user_session ADD CONSTRAINT user_session_door_is_open_door
    CHECK (portal_project_id IS NULL OR proof = 'open_door');

-- Leaving an organization is always allowed; arriving in another one is only
-- for a session whose proof vouches for the person everywhere.
-- +goose StatementBegin
CREATE FUNCTION session_stays_home() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.current_org_id IS NOT NULL AND NEW.current_org_id IS DISTINCT FROM OLD.current_org_id
       AND OLD.proof NOT IN ('password', 'invite') THEN
        RAISE EXCEPTION 'a session opened by % reaches only the organization it was opened for', OLD.proof
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER session_stays_home BEFORE UPDATE OF current_org_id, proof ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_stays_home();

-- Wrong codes are counted per address for an hour across reissues, so asking
-- for a new code does not buy more guesses.
ALTER TABLE portal_code ADD COLUMN window_attempts int NOT NULL DEFAULT 0,
                        ADD COLUMN window_started timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE portal_code DROP COLUMN IF EXISTS window_started, DROP COLUMN IF EXISTS window_attempts;
DROP TRIGGER IF EXISTS session_stays_home ON user_session;
DROP FUNCTION IF EXISTS session_stays_home();
ALTER TABLE user_session DROP CONSTRAINT IF EXISTS user_session_door_is_open_door;
ALTER TABLE user_session DROP COLUMN IF EXISTS proof;
