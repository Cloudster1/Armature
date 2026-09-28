-- +goose Up
-- A session remembers which organization its proof was made for, apart from
-- the one it is looking at now, so that leaving home and coming back can be
-- told from arriving somewhere the proof never vouched for.
ALTER TABLE user_session ADD COLUMN proof_org_id uuid REFERENCES org(id) ON DELETE SET NULL;
UPDATE user_session SET proof_org_id = current_org_id;

-- Where a session may go. A password or an accepted invitation vouches for
-- the person everywhere. A provider's sign-in vouches for them wherever that
-- provider is trusted, and in an organization they own, whose sign-in policy
-- is theirs to set. A mailed code and an open door vouch for one place.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION session_reaches(proof text, proof_org uuid, person uuid, target uuid) RETURNS boolean
    LANGUAGE sql STABLE AS $$
    SELECT proof IN ('password', 'invite')
        OR target = proof_org
        OR (proof = 'oidc' AND (
            EXISTS (SELECT 1 FROM org_member
                    WHERE org_id = target AND user_id = person AND org_role = 'owner')
            OR EXISTS (SELECT 1 FROM oidc_provider home
                       JOIN oidc_provider away ON away.issuer = home.issuer
                       WHERE home.org_id = proof_org AND away.org_id = target
                         AND home.enabled AND away.enabled)))
$$;

CREATE OR REPLACE FUNCTION session_stays_home() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
    -- The organization going, which nulls the column, is the one way it changes.
    IF NEW.proof_org_id IS NOT NULL AND NEW.proof_org_id IS DISTINCT FROM OLD.proof_org_id THEN
        RAISE EXCEPTION 'a session keeps the organization it was proven for' USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.current_org_id IS NOT NULL AND NEW.current_org_id IS DISTINCT FROM OLD.current_org_id
       AND NOT session_reaches(OLD.proof, OLD.proof_org_id, OLD.user_id, NEW.current_org_id) THEN
        RAISE EXCEPTION 'a session opened by % does not reach that organization', OLD.proof
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS session_stays_home ON user_session;
CREATE TRIGGER session_stays_home BEFORE UPDATE OF current_org_id, proof, proof_org_id ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_stays_home();

-- +goose Down
DROP TRIGGER IF EXISTS session_stays_home ON user_session;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION session_stays_home() RETURNS trigger LANGUAGE plpgsql AS $$
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
DROP FUNCTION IF EXISTS session_reaches(text, uuid, uuid, uuid);
ALTER TABLE user_session DROP COLUMN IF EXISTS proof_org_id;
