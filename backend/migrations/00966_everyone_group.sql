-- +goose Up
-- A built-in group Everyone holds every member of the installation, never a
-- portal customer (#470), so a grant meant for all members names one group,
-- as Stator's and Exciter's "everyone" does. Its members follow the account's
-- standing; nobody adds or removes them by hand, and it cannot be renamed,
-- edited or deleted.

ALTER TABLE user_group ADD COLUMN builtin text
    CONSTRAINT user_group_builtin_known CHECK (builtin IN ('everyone'));
CREATE UNIQUE INDEX user_group_builtin_idx ON user_group (builtin) WHERE builtin IS NOT NULL;

-- A group somebody made under the name keeps its members and grants under another.
UPDATE user_group SET name = name || ' (made by hand)' WHERE lower(name) = 'everyone';

INSERT INTO user_group (name, description, builtin)
VALUES ('Everyone', 'Every member of the installation, not portal customers.', 'everyone');

INSERT INTO group_member (group_id, user_id)
SELECT g.id, u.id FROM user_group g, app_user u
WHERE g.builtin = 'everyone' AND u.standing IN ('owner', 'admin', 'member');

-- +goose StatementBegin
CREATE FUNCTION app_user_joins_everyone() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.standing IN ('owner', 'admin', 'member') THEN
        INSERT INTO group_member (group_id, user_id)
        SELECT id, NEW.id FROM user_group WHERE builtin = 'everyone'
        ON CONFLICT DO NOTHING;
    ELSIF TG_OP = 'UPDATE' THEN
        DELETE FROM group_member m USING user_group g
        WHERE m.group_id = g.id AND g.builtin = 'everyone' AND m.user_id = NEW.id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER app_user_joins_everyone AFTER INSERT OR UPDATE OF standing ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_joins_everyone();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION group_member_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing IS NOT NULL) THEN
        RAISE EXCEPTION 'only a member here can join a group'
            USING ERRCODE = 'check_violation';
    END IF;
    IF EXISTS (SELECT 1 FROM user_group WHERE id = NEW.group_id AND builtin = 'everyone')
       AND NOT EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing IN ('owner', 'admin', 'member')) THEN
        RAISE EXCEPTION 'Everyone holds the members here, never a portal customer'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- An account that is gone, or whose membership ended, leaves Everyone; a
-- member still here stays in it, whoever asks.
-- +goose StatementBegin
CREATE FUNCTION group_member_keeps_everyone() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM user_group WHERE id = OLD.group_id AND builtin = 'everyone')
       AND EXISTS (SELECT 1 FROM app_user WHERE id = OLD.user_id AND standing IN ('owner', 'admin', 'member')) THEN
        RAISE EXCEPTION 'Everyone holds every member here, so nobody is taken out of it by hand'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER group_member_keeps_everyone BEFORE DELETE ON group_member
    FOR EACH ROW EXECUTE FUNCTION group_member_keeps_everyone();

-- +goose StatementBegin
CREATE FUNCTION user_group_builtin_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.builtin IS NOT NULL THEN
            RAISE EXCEPTION 'the built-in group % cannot be deleted', OLD.name
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.builtin IS DISTINCT FROM OLD.builtin
       OR (OLD.builtin IS NOT NULL AND (NEW.name, NEW.description, NEW.source, NEW.external_ref)
           IS DISTINCT FROM (OLD.name, OLD.description, OLD.source, OLD.external_ref)) THEN
        RAISE EXCEPTION 'the built-in group % cannot be changed', OLD.name
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER user_group_builtin_guard BEFORE UPDATE OR DELETE ON user_group
    FOR EACH ROW EXECUTE FUNCTION user_group_builtin_guard();

-- Administration given to everybody at once would make every member an
-- administrator, which Stator and Exciter refuse for "everyone" too.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NOT NULL
       AND (SELECT installation_wide_only FROM role WHERE key = NEW.role) THEN
        RAISE EXCEPTION 'that role answers for the whole installation and cannot be granted over one project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'a role can only be granted to a member here'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.group_id IS NOT NULL
       AND EXISTS (SELECT 1 FROM user_group WHERE id = NEW.group_id AND builtin = 'everyone')
       AND (SELECT 'org.administer' = ANY(permissions) FROM role WHERE key = NEW.role) THEN
        RAISE EXCEPTION 'Everyone cannot administer the installation'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_everyone_administers';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION role_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        -- A built-in role goes only with the installation, whose row is
        -- already gone by the time the cascade reaches here.
        IF OLD.builtin AND EXISTS (SELECT 1 FROM installation) THEN
            RAISE EXCEPTION 'a built-in role cannot be deleted'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.key <> OLD.key OR NEW.builtin <> OLD.builtin THEN
            RAISE EXCEPTION 'a role''s key and standing are fixed'
                USING ERRCODE = 'check_violation';
        END IF;
        IF 'org.administer' = ANY(NEW.permissions) AND NOT ('org.administer' = ANY(OLD.permissions))
           AND EXISTS (SELECT 1 FROM role_assignment a JOIN user_group g ON g.id = a.group_id
                       WHERE a.role = NEW.key AND g.builtin = 'everyone') THEN
            RAISE EXCEPTION 'Everyone holds this role, so it cannot administer the installation'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'role_assignment_everyone_administers';
        END IF;
    END IF;
    IF NEW.key = 'global_administrator' AND NOT ('org.administer' = ANY(NEW.permissions)) THEN
        RAISE EXCEPTION 'the global administrator keeps administering the installation'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION role_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.builtin AND EXISTS (SELECT 1 FROM installation) THEN
            RAISE EXCEPTION 'a built-in role cannot be deleted'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.key <> OLD.key OR NEW.builtin <> OLD.builtin THEN
            RAISE EXCEPTION 'a role''s key and standing are fixed'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    IF NEW.key = 'global_administrator' AND NOT ('org.administer' = ANY(NEW.permissions)) THEN
        RAISE EXCEPTION 'the global administrator keeps administering the installation'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NOT NULL
       AND (SELECT installation_wide_only FROM role WHERE key = NEW.role) THEN
        RAISE EXCEPTION 'that role answers for the whole installation and cannot be granted over one project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'a role can only be granted to a member here'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION group_member_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing IS NOT NULL) THEN
        RAISE EXCEPTION 'only a member here can join a group'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER user_group_builtin_guard ON user_group;
DROP FUNCTION user_group_builtin_guard();
DROP TRIGGER group_member_keeps_everyone ON group_member;
DROP FUNCTION group_member_keeps_everyone();
DROP TRIGGER app_user_joins_everyone ON app_user;
DROP FUNCTION app_user_joins_everyone();
DELETE FROM user_group WHERE builtin IS NOT NULL;
ALTER TABLE user_group DROP COLUMN builtin;
