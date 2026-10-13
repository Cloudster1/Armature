-- +goose Up
-- The installation keeps at least one active administrator, owner or admin,
-- rather than an owner: an owner may now be switched off while an
-- administrator stays. The service refuses the same, because a column the
-- code forgets to check is not a rule.
-- +goose StatementBegin
CREATE FUNCTION app_user_keeps_an_administrator() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    orphan text;
BEGIN
    IF OLD.is_active AND NOT NEW.is_active THEN
        SELECT o.name INTO orphan
        FROM org_member m JOIN org o ON o.id = m.org_id
        WHERE m.user_id = NEW.id AND m.org_role IN ('owner', 'admin') AND o.archived_at IS NULL
          AND NOT EXISTS (SELECT 1 FROM org_member x JOIN app_user xu ON xu.id = x.user_id
                          WHERE x.org_id = m.org_id AND x.org_role IN ('owner', 'admin')
                            AND x.user_id <> NEW.id AND xu.is_active)
        ORDER BY o.name LIMIT 1;
        IF orphan IS NOT NULL THEN
            RAISE EXCEPTION 'the last active administrator of % cannot be switched off', orphan
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
DROP TRIGGER app_user_keeps_an_owner ON app_user;
DROP FUNCTION app_user_keeps_an_owner();
CREATE TRIGGER app_user_keeps_an_administrator BEFORE UPDATE OF is_active ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_keeps_an_administrator();

-- +goose Down
DROP TRIGGER IF EXISTS app_user_keeps_an_administrator ON app_user;
DROP FUNCTION IF EXISTS app_user_keeps_an_administrator();
-- +goose StatementBegin
CREATE FUNCTION app_user_keeps_an_owner() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    orphan text;
BEGIN
    IF OLD.is_active AND NOT NEW.is_active THEN
        SELECT o.name INTO orphan
        FROM org_member m JOIN org o ON o.id = m.org_id
        WHERE m.user_id = NEW.id AND m.org_role = 'owner' AND o.archived_at IS NULL
          AND NOT EXISTS (SELECT 1 FROM org_member x JOIN app_user xu ON xu.id = x.user_id
                          WHERE x.org_id = m.org_id AND x.org_role = 'owner'
                            AND x.user_id <> NEW.id AND xu.is_active)
        ORDER BY o.name LIMIT 1;
        IF orphan IS NOT NULL THEN
            RAISE EXCEPTION 'the last owner of % cannot be switched off', orphan
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER app_user_keeps_an_owner BEFORE UPDATE OF is_active ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_keeps_an_owner();
