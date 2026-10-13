-- +goose Up
-- A session no longer names an organization: it acts in the person's own, and
-- there is nowhere else to reach. The columns stay until the tenant goes (#218),
-- empty, and a check keeps them so, because a column the code forgets is not a rule.
DROP TRIGGER session_stays_home ON user_session;
DROP FUNCTION session_stays_home();
DROP FUNCTION session_reaches(text, uuid, uuid, uuid);
UPDATE user_session SET current_org_id = NULL, proof_org_id = NULL
WHERE current_org_id IS NOT NULL OR proof_org_id IS NOT NULL;
ALTER TABLE user_session ADD CONSTRAINT user_session_names_no_organization
    CHECK (current_org_id IS NULL AND proof_org_id IS NULL);

-- What proved a session is still what it was opened with.
-- +goose StatementBegin
CREATE FUNCTION session_keeps_its_proof() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER session_keeps_its_proof BEFORE UPDATE OF proof ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_keeps_its_proof();

-- +goose Down
DROP TRIGGER session_keeps_its_proof ON user_session;
DROP FUNCTION session_keeps_its_proof();
ALTER TABLE user_session DROP CONSTRAINT user_session_names_no_organization;
-- +goose StatementBegin
CREATE FUNCTION session_reaches(proof text, proof_org uuid, person uuid, target uuid) RETURNS boolean
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

CREATE FUNCTION session_stays_home() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.proof IS DISTINCT FROM OLD.proof THEN
        RAISE EXCEPTION 'a session keeps the proof it was opened with' USING ERRCODE = 'check_violation';
    END IF;
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
CREATE TRIGGER session_stays_home BEFORE UPDATE OF current_org_id, proof, proof_org_id ON user_session
    FOR EACH ROW EXECUTE FUNCTION session_stays_home();
