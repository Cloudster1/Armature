-- +goose Up
-- The installation always keeps one global administrator (#408). The service
-- refused revoking the last one, but two revokes at once each counted the other
-- as the one that stays, and nothing here refused it either. The database now
-- refuses taking away, or changing, the last global_administrator assignment.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION role_assignment_keeps_an_administrator() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.role = OLD.role THEN
        RETURN NULL;
    END IF;
    -- The same lock perm.Store.Revoke takes: the second of two removals waits,
    -- then counts with what the first one committed.
    PERFORM pg_advisory_xact_lock(hashtext('armature.global_administrators'));
    IF NOT EXISTS (SELECT 1 FROM role_assignment WHERE role = 'global_administrator') THEN
        RAISE EXCEPTION 'this is the last global administrator, so granting another one has to come first'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_keeps_an_administrator';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER role_assignment_keeps_an_administrator
    AFTER DELETE OR UPDATE OF role ON role_assignment
    FOR EACH ROW WHEN (OLD.role = 'global_administrator')
    EXECUTE FUNCTION role_assignment_keeps_an_administrator();

-- +goose Down
DROP TRIGGER IF EXISTS role_assignment_keeps_an_administrator ON role_assignment;
DROP FUNCTION IF EXISTS role_assignment_keeps_an_administrator();
