-- +goose Up
-- An installation holds one organization, so no row can cross into another,
-- and a trigger that says so says nothing (#218). Each rule keeps what it
-- holds a row to, its project, its service desk or its member, and decides it
-- without org_id. The membership checks read org_member by person.
--
-- These triggers held a row to its organization alone; the foreign keys keep
-- what they also said, that the row they point at exists.
DROP TRIGGER automation_rule_project_in_org ON automation_rule;
DROP FUNCTION automation_rule_project_in_org();
DROP TRIGGER dashboard_share_check ON dashboard_share;
DROP FUNCTION dashboard_share_guard();
DROP TRIGGER holiday_same_org ON holiday;
DROP FUNCTION holiday_same_org();
DROP TRIGGER issue_link_check ON issue_link;
DROP FUNCTION issue_link_guard();
DROP TRIGGER issue_old_key_same_org ON issue_old_key;
DROP FUNCTION issue_old_key_same_org();
DROP TRIGGER issue_remote_link_same_org ON issue_remote_link;
DROP FUNCTION issue_remote_link_same_org();
DROP TRIGGER notification_same_org ON notification;
DROP FUNCTION notification_same_org();
DROP TRIGGER project_scheme_check ON project;
DROP FUNCTION project_scheme_guard();
DROP TRIGGER workflow_scheme_item_check ON workflow_scheme_item;
DROP FUNCTION workflow_scheme_item_guard();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_milestone_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    milestone_project uuid;
    milestone_closed  timestamptz;
BEGIN
    IF NEW.milestone_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id, closed_at INTO milestone_project, milestone_closed
      FROM milestone WHERE id = NEW.milestone_id;

    IF milestone_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'an issue can only be assigned to a milestone in its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF milestone_closed IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.milestone_id IS DISTINCT FROM NEW.milestone_id) THEN
        RAISE EXCEPTION 'that milestone is closed'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_sprint_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    sprint_project uuid;
    sprint_team    uuid;
    joined_state   sprint_state;
BEGIN
    IF NEW.sprint_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id, state, team_id INTO sprint_project, joined_state, sprint_team
      FROM sprint WHERE id = NEW.sprint_id;

    IF sprint_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'an issue can only join a sprint in its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    -- Work cannot be added to a sprint that is already over; the report on it
    -- has been written.
    IF joined_state = 'closed' AND (TG_OP = 'INSERT' OR OLD.sprint_id IS DISTINCT FROM NEW.sprint_id) THEN
        RAISE EXCEPTION 'that sprint is over'
            USING ERRCODE = 'check_violation';
    END IF;
    -- A closed sprint is history and keeps its work whoever carries it now. A
    -- team already gone is a delete still cascading, where both sides clear.
    IF joined_state <> 'closed' AND sprint_team IS DISTINCT FROM NEW.team_id
       AND (sprint_team IS NULL OR EXISTS (SELECT 1 FROM team WHERE id = sprint_team)) THEN
        RAISE EXCEPTION 'an issue can only be in a sprint its own team plans'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION team_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    team_project uuid;
BEGIN
    IF NEW.team_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id INTO team_project FROM team WHERE id = NEW.team_id;
    IF team_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'that team is not in this project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_version_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    issue_project    uuid;
    version_project  uuid;
    version_archived timestamptz;
BEGIN
    SELECT project_id INTO issue_project FROM issue WHERE id = NEW.issue_id;
    SELECT project_id, archived_at INTO version_project, version_archived FROM version WHERE id = NEW.version_id;
    IF version_project IS DISTINCT FROM issue_project THEN
        RAISE EXCEPTION 'an issue can only name a version of its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF version_archived IS NOT NULL THEN
        RAISE EXCEPTION 'that version is archived'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_component_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    issue_project     uuid;
    component_project uuid;
BEGIN
    SELECT project_id INTO issue_project FROM issue WHERE id = NEW.issue_id;
    SELECT project_id INTO component_project FROM component WHERE id = NEW.component_id;
    IF component_project IS DISTINCT FROM issue_project THEN
        RAISE EXCEPTION 'an issue can only be in a component of its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_field_value_same_project() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    issue_project uuid;
    field_project uuid;
BEGIN
    SELECT project_id INTO issue_project FROM issue WHERE id = NEW.issue_id;
    SELECT project_id INTO field_project FROM custom_field WHERE id = NEW.field_id;
    IF field_project IS NOT NULL AND issue_project IS DISTINCT FROM field_project THEN
        RAISE EXCEPTION 'field % is not a field of the project issue % is in', NEW.field_id, NEW.issue_id
            USING ERRCODE = 'check_violation', CONSTRAINT = 'issue_field_value_same_project';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION kb_article_service_project() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM project WHERE id = NEW.project_id AND kind = 'service') THEN
        RAISE EXCEPTION 'articles belong to a service desk project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION csat_rating_service_issue() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM issue i JOIN project p ON p.id = i.project_id
        WHERE i.id = NEW.issue_id AND p.kind = 'service'
    ) THEN
        RAISE EXCEPTION 'a rating belongs to a request of a service desk'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION custom_field_name_unshadowed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NULL THEN
        IF EXISTS (SELECT 1 FROM custom_field f
                   WHERE f.project_id IS NOT NULL
                     AND lower(f.name) = lower(NEW.name) AND f.id <> NEW.id) THEN
            RAISE EXCEPTION 'a project already has a field named %', NEW.name
                USING ERRCODE = 'unique_violation', CONSTRAINT = 'custom_field_name_unshadowed';
        END IF;
    ELSE
        IF EXISTS (SELECT 1 FROM custom_field f
                   WHERE f.project_id IS NULL
                     AND lower(f.name) = lower(NEW.name) AND f.id <> NEW.id) THEN
            RAISE EXCEPTION 'the installation already has a field named %', NEW.name
                USING ERRCODE = 'unique_violation', CONSTRAINT = 'custom_field_name_unshadowed';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_hierarchy_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    child_level  smallint;
    child_type   text;
    parent_level smallint;
    parent_type  text;
    parent_proj  uuid;
BEGIN
    SELECT hierarchy_level, name INTO child_level, child_type
    FROM issue_type WHERE id = NEW.issue_type_id;

    IF NEW.parent_id IS NULL THEN
        IF child_level < 0 THEN
            RAISE EXCEPTION 'a % only exists underneath another issue', lower(child_type)
                USING ERRCODE = 'check_violation';
        END IF;
    ELSE
        SELECT t.hierarchy_level, t.name, i.project_id
        INTO parent_level, parent_type, parent_proj
        FROM issue i JOIN issue_type t ON t.id = i.issue_type_id
        WHERE i.id = NEW.parent_id;

        IF NOT FOUND THEN
            RAISE EXCEPTION 'that parent issue does not exist'
                USING ERRCODE = 'foreign_key_violation';
        END IF;
        IF parent_proj <> NEW.project_id THEN
            RAISE EXCEPTION 'a parent has to be in the same project as its child'
                USING ERRCODE = 'check_violation';
        END IF;
        IF parent_level <> child_level + 1 THEN
            RAISE EXCEPTION 'a % cannot be the parent of a %', lower(parent_type), lower(child_type)
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    -- Changing an issue's own type moves it in the tree, which can strand the
    -- children hanging off it. They are not part of this statement, so nothing
    -- else would notice.
    IF TG_OP = 'UPDATE' AND NEW.issue_type_id IS DISTINCT FROM OLD.issue_type_id
       AND EXISTS (
           SELECT 1 FROM issue c
           JOIN issue_type ct ON ct.id = c.issue_type_id
           WHERE c.parent_id = NEW.id AND ct.hierarchy_level <> child_level - 1)
    THEN
        RAISE EXCEPTION 'this issue has children that a % cannot have', lower(child_type)
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION absence_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, can be away'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION member_schedule_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- A calendar that goes clears itself from the weeks that kept it, whoever
    -- those people are by now.
    IF TG_OP = 'UPDATE' AND NEW.calendar_id IS NULL AND OLD.calendar_id IS NOT NULL
       AND NEW.user_id = OLD.user_id AND NEW.minutes = OLD.minutes THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, has a working week'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION project_allocation_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, gives a project a share of their week'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION team_member_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, can join a team'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION group_member_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM org_member WHERE user_id = NEW.user_id) THEN
        RAISE EXCEPTION 'only a member here can join a group'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION saved_filter_share_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.owner_id AND org_role = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a saved filter'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION theme_share_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.owner_id AND org_role = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a theme'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION theme_unshared_default() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE org SET default_theme_id = NULL WHERE default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NOT NULL
       AND (SELECT org_wide_only FROM org_role WHERE key = NEW.role) THEN
        RAISE EXCEPTION 'that role answers for the whole installation and cannot be granted over one project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id
    ) THEN
        RAISE EXCEPTION 'a role can only be granted to a member here'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION org_role_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        -- A built-in role goes only with the installation, whose row is
        -- already gone by the time the cascade reaches here.
        IF OLD.builtin AND EXISTS (SELECT 1 FROM org) THEN
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

CREATE OR REPLACE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.is_active AND NOT NEW.is_active
       AND EXISTS (SELECT 1 FROM org_member WHERE user_id = NEW.id AND org_role IN ('owner', 'admin'))
       AND NOT EXISTS (SELECT 1 FROM org_member x JOIN app_user xu ON xu.id = x.user_id
                       WHERE x.org_role IN ('owner', 'admin') AND x.user_id <> NEW.id AND xu.is_active) THEN
        RAISE EXCEPTION 'the last active administrator cannot be switched off'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION workflow_scheme_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    default_id uuid;
BEGIN
    -- Nothing is mapped yet, or the installation is going away with its schemes.
    IF NOT EXISTS (SELECT 1 FROM workflow_scheme) THEN
        RETURN NULL;
    END IF;

    SELECT id INTO default_id FROM workflow_scheme WHERE is_default;
    IF default_id IS NULL THEN
        RAISE EXCEPTION 'the installation has to keep one default workflow scheme'
            USING ERRCODE = 'check_violation';
    END IF;

    -- Without a catch-all the default scheme is not a backstop, and an issue
    -- type nobody mapped would have no workflow to start in.
    IF NOT EXISTS (
        SELECT 1 FROM workflow_scheme_item
         WHERE scheme_id = default_id AND issue_type_id IS NULL
    ) THEN
        RAISE EXCEPTION 'the default workflow scheme has to map the issue types it does not name'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION attachment_leaves_tombstone() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO attachment_tombstone (object_key) VALUES (OLD.object_key)
    ON CONFLICT (object_key) DO NOTHING;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- No trigger waits on org_id changing, so dropping the column later takes
-- none of them with it.
DROP TRIGGER issue_milestone_check ON issue;
CREATE TRIGGER issue_milestone_check BEFORE INSERT OR UPDATE OF milestone_id, project_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_milestone_guard();
DROP TRIGGER issue_sprint_check ON issue;
CREATE TRIGGER issue_sprint_check BEFORE INSERT OR UPDATE OF sprint_id, team_id, project_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_sprint_guard();
DROP TRIGGER issue_team_check ON issue;
CREATE TRIGGER issue_team_check BEFORE INSERT OR UPDATE OF team_id, project_id ON issue
    FOR EACH ROW EXECUTE FUNCTION team_scope_guard();
DROP TRIGGER board_team_check ON board;
CREATE TRIGGER board_team_check BEFORE INSERT OR UPDATE OF team_id, project_id ON board
    FOR EACH ROW EXECUTE FUNCTION team_scope_guard();
DROP TRIGGER sprint_team_check ON sprint;
CREATE TRIGGER sprint_team_check BEFORE INSERT OR UPDATE OF team_id, project_id ON sprint
    FOR EACH ROW EXECUTE FUNCTION team_scope_guard();

-- Forgetting who granted a role, as their account goes, grants nothing.
DROP TRIGGER role_assignment_check ON role_assignment;
CREATE TRIGGER role_assignment_check BEFORE INSERT OR UPDATE OF role, project_id, user_id, group_id ON role_assignment
    FOR EACH ROW EXECUTE FUNCTION role_assignment_guard();

-- +goose Down
-- Each trigger holds its row to its organization again, as the migrations
-- before this one made it.
DROP TRIGGER role_assignment_check ON role_assignment;
CREATE TRIGGER role_assignment_check BEFORE INSERT OR UPDATE ON role_assignment
    FOR EACH ROW EXECUTE FUNCTION role_assignment_guard();
DROP TRIGGER issue_milestone_check ON issue;
CREATE TRIGGER issue_milestone_check BEFORE INSERT OR UPDATE OF milestone_id, project_id, org_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_milestone_guard();
DROP TRIGGER issue_sprint_check ON issue;
CREATE TRIGGER issue_sprint_check BEFORE INSERT OR UPDATE OF sprint_id, team_id, project_id, org_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_sprint_guard();
DROP TRIGGER issue_team_check ON issue;
CREATE TRIGGER issue_team_check BEFORE INSERT OR UPDATE OF team_id, project_id, org_id ON issue
    FOR EACH ROW EXECUTE FUNCTION team_scope_guard();
DROP TRIGGER board_team_check ON board;
CREATE TRIGGER board_team_check BEFORE INSERT OR UPDATE OF team_id, project_id, org_id ON board
    FOR EACH ROW EXECUTE FUNCTION team_scope_guard();
DROP TRIGGER sprint_team_check ON sprint;
CREATE TRIGGER sprint_team_check BEFORE INSERT OR UPDATE OF team_id, project_id, org_id ON sprint
    FOR EACH ROW EXECUTE FUNCTION team_scope_guard();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION absence_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM org_member
         WHERE org_id = NEW.org_id AND user_id = NEW.user_id
           AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member of the organization is away from it'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
LANGUAGE plpgsql AS $$
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

CREATE OR REPLACE FUNCTION attachment_leaves_tombstone() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO attachment_tombstone (object_key, org_id)
    VALUES (OLD.object_key, (SELECT id FROM org WHERE id = OLD.org_id))
    ON CONFLICT (object_key) DO NOTHING;
    RETURN OLD;
END;
$$;

CREATE FUNCTION automation_rule_project_in_org() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM project WHERE id = NEW.project_id AND org_id = NEW.org_id
    ) THEN
        RAISE EXCEPTION 'a rule can only watch a project of its own organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION csat_rating_service_issue() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM issue i JOIN project p ON p.id = i.project_id
        WHERE i.id = NEW.issue_id AND i.org_id = NEW.org_id AND p.kind = 'service'
    ) THEN
        RAISE EXCEPTION 'a rating belongs to a request of a service desk'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION custom_field_name_unshadowed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NULL THEN
        IF EXISTS (SELECT 1 FROM custom_field f
                   WHERE f.org_id = NEW.org_id AND f.project_id IS NOT NULL
                     AND lower(f.name) = lower(NEW.name) AND f.id <> NEW.id) THEN
            RAISE EXCEPTION 'a project already has a field named %', NEW.name
                USING ERRCODE = 'unique_violation', CONSTRAINT = 'custom_field_name_unshadowed';
        END IF;
    ELSE
        IF EXISTS (SELECT 1 FROM custom_field f
                   WHERE f.org_id = NEW.org_id AND f.project_id IS NULL
                     AND lower(f.name) = lower(NEW.name) AND f.id <> NEW.id) THEN
            RAISE EXCEPTION 'the organization already has a field named %', NEW.name
                USING ERRCODE = 'unique_violation', CONSTRAINT = 'custom_field_name_unshadowed';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION dashboard_share_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    dashboard_org uuid;
BEGIN
    SELECT org_id INTO dashboard_org FROM dashboard WHERE id = NEW.dashboard_id;
    IF dashboard_org IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a link can only open a dashboard of its own organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION group_member_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    group_org uuid;
BEGIN
    SELECT org_id INTO group_org FROM user_group WHERE id = NEW.group_id;
    IF group_org IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a group member cannot cross organizations'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id
    ) THEN
        RAISE EXCEPTION 'only a member of the organization can join one of its groups'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION holiday_same_org() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM holiday_calendar WHERE id = NEW.calendar_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'a holiday must belong to the same organization as its calendar'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_component_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    issue_project     uuid;
    component_project uuid;
    component_org     uuid;
BEGIN
    SELECT project_id INTO issue_project FROM issue WHERE id = NEW.issue_id;
    SELECT project_id, org_id INTO component_project, component_org FROM component WHERE id = NEW.component_id;
    IF component_org IS DISTINCT FROM NEW.org_id OR component_project IS DISTINCT FROM issue_project THEN
        RAISE EXCEPTION 'an issue can only be in a component of its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_field_value_same_project() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    issue_project uuid;
    issue_org     uuid;
    field_project uuid;
    field_org     uuid;
BEGIN
    SELECT project_id, org_id INTO issue_project, issue_org FROM issue WHERE id = NEW.issue_id;
    SELECT project_id, org_id INTO field_project, field_org FROM custom_field WHERE id = NEW.field_id;
    IF field_org IS DISTINCT FROM issue_org
       OR (field_project IS NOT NULL AND issue_project IS DISTINCT FROM field_project) THEN
        RAISE EXCEPTION 'field % is not a field of the project issue % is in', NEW.field_id, NEW.issue_id
            USING ERRCODE = 'check_violation', CONSTRAINT = 'issue_field_value_same_project';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_hierarchy_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    child_level  smallint;
    child_type   text;
    parent_level smallint;
    parent_type  text;
    parent_proj  uuid;
BEGIN
    SELECT hierarchy_level, name INTO child_level, child_type
    FROM issue_type WHERE id = NEW.issue_type_id;

    IF NEW.parent_id IS NULL THEN
        IF child_level < 0 THEN
            RAISE EXCEPTION 'a % only exists underneath another issue', lower(child_type)
                USING ERRCODE = 'check_violation';
        END IF;
    ELSE
        -- Row level security applies to this lookup, so a parent in another
        -- organization is not merely refused: it is not there at all.
        SELECT t.hierarchy_level, t.name, i.project_id
        INTO parent_level, parent_type, parent_proj
        FROM issue i JOIN issue_type t ON t.id = i.issue_type_id
        WHERE i.id = NEW.parent_id;

        IF NOT FOUND THEN
            RAISE EXCEPTION 'that parent issue does not exist'
                USING ERRCODE = 'foreign_key_violation';
        END IF;
        IF parent_proj <> NEW.project_id THEN
            RAISE EXCEPTION 'a parent has to be in the same project as its child'
                USING ERRCODE = 'check_violation';
        END IF;
        IF parent_level <> child_level + 1 THEN
            RAISE EXCEPTION 'a % cannot be the parent of a %', lower(parent_type), lower(child_type)
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    -- Changing an issue's own type moves it in the tree, which can strand the
    -- children hanging off it. They are not part of this statement, so nothing
    -- else would notice.
    IF TG_OP = 'UPDATE' AND NEW.issue_type_id IS DISTINCT FROM OLD.issue_type_id
       AND EXISTS (
           SELECT 1 FROM issue c
           JOIN issue_type ct ON ct.id = c.issue_type_id
           WHERE c.parent_id = NEW.id AND ct.hierarchy_level <> child_level - 1)
    THEN
        RAISE EXCEPTION 'this issue has children that a % cannot have', lower(child_type)
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE FUNCTION issue_link_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    source_org uuid;
    target_org uuid;
BEGIN
    SELECT org_id INTO source_org FROM issue WHERE id = NEW.source_id;
    SELECT org_id INTO target_org FROM issue WHERE id = NEW.target_id;

    IF source_org IS NULL OR target_org IS NULL THEN
        RAISE EXCEPTION 'both ends of a link have to exist'
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF source_org <> target_org OR source_org <> NEW.org_id THEN
        RAISE EXCEPTION 'a link cannot cross organizations'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_milestone_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    milestone_project uuid;
    milestone_org     uuid;
    milestone_closed  timestamptz;
BEGIN
    IF NEW.milestone_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id, org_id, closed_at INTO milestone_project, milestone_org, milestone_closed
      FROM milestone WHERE id = NEW.milestone_id;

    IF milestone_org IS DISTINCT FROM NEW.org_id OR milestone_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'an issue can only be assigned to a milestone in its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF milestone_closed IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.milestone_id IS DISTINCT FROM NEW.milestone_id) THEN
        RAISE EXCEPTION 'that milestone is closed'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION issue_old_key_same_org() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM issue WHERE id = NEW.issue_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'an old key must belong to the same organization as its issue'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION issue_remote_link_same_org() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM issue WHERE id = NEW.issue_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'a page link must belong to the same organization as its issue'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_sprint_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    sprint_project uuid;
    sprint_org     uuid;
    sprint_team    uuid;
    joined_state   sprint_state;
BEGIN
    IF NEW.sprint_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id, org_id, state, team_id INTO sprint_project, sprint_org, joined_state, sprint_team
      FROM sprint WHERE id = NEW.sprint_id;

    IF sprint_org IS DISTINCT FROM NEW.org_id OR sprint_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'an issue can only join a sprint in its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    -- Work cannot be added to a sprint that is already over; the report on it
    -- has been written.
    IF joined_state = 'closed' AND (TG_OP = 'INSERT' OR OLD.sprint_id IS DISTINCT FROM NEW.sprint_id) THEN
        RAISE EXCEPTION 'that sprint is over'
            USING ERRCODE = 'check_violation';
    END IF;
    -- A closed sprint is history and keeps its work whoever carries it now. A
    -- team already gone is a delete still cascading, where both sides clear.
    IF joined_state <> 'closed' AND sprint_team IS DISTINCT FROM NEW.team_id
       AND (sprint_team IS NULL OR EXISTS (SELECT 1 FROM team WHERE id = sprint_team)) THEN
        RAISE EXCEPTION 'an issue can only be in a sprint its own team plans'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION issue_version_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    issue_project    uuid;
    version_project  uuid;
    version_org      uuid;
    version_archived timestamptz;
BEGIN
    SELECT project_id INTO issue_project FROM issue WHERE id = NEW.issue_id;
    SELECT project_id, org_id, archived_at INTO version_project, version_org, version_archived FROM version WHERE id = NEW.version_id;
    IF version_org IS DISTINCT FROM NEW.org_id OR version_project IS DISTINCT FROM issue_project THEN
        RAISE EXCEPTION 'an issue can only name a version of its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF version_archived IS NOT NULL THEN
        RAISE EXCEPTION 'that version is archived'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION kb_article_service_project() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM project WHERE id = NEW.project_id AND org_id = NEW.org_id AND kind = 'service') THEN
        RAISE EXCEPTION 'articles belong to a service desk project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION member_schedule_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.calendar_id IS NULL AND OLD.calendar_id IS NOT NULL
       AND NEW.org_id = OLD.org_id AND NEW.user_id = OLD.user_id AND NEW.minutes = OLD.minutes THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM org_member
         WHERE org_id = NEW.org_id AND user_id = NEW.user_id
           AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member of the organization has a working week in it'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.calendar_id IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM holiday_calendar WHERE id = NEW.calendar_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'a working week must keep a calendar of its own organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION notification_same_org() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.issue_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM issue WHERE id = NEW.issue_id AND org_id = NEW.org_id
    ) THEN
        RAISE EXCEPTION 'a notification must belong to the same organization as its issue'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION org_role_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        -- A built-in role goes only with its organization, whose row is
        -- already gone by the time the cascade reaches here.
        IF OLD.builtin AND EXISTS (SELECT 1 FROM org WHERE id = OLD.org_id) THEN
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
        RAISE EXCEPTION 'the global administrator keeps administering the organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION project_allocation_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM project WHERE id = NEW.project_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'a share must be of a project in the same organization'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM org_member
         WHERE org_id = NEW.org_id AND user_id = NEW.user_id
           AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member of the organization gives a project a share of their week'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION project_scheme_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    scheme_org uuid;
BEGIN
    IF NEW.workflow_scheme_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT org_id INTO scheme_org FROM workflow_scheme WHERE id = NEW.workflow_scheme_id;
    IF scheme_org IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a project cannot borrow another organization''s workflow scheme'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    scope_org   uuid;
    subject_org uuid;
    whole_org   boolean;
BEGIN
    IF NEW.project_id IS NOT NULL THEN
        SELECT org_id INTO scope_org FROM project WHERE id = NEW.project_id;
        IF scope_org IS DISTINCT FROM NEW.org_id THEN
            RAISE EXCEPTION 'a role cannot be granted over another organization''s project'
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT org_wide_only INTO whole_org FROM org_role WHERE org_id = NEW.org_id AND key = NEW.role;
        IF whole_org THEN
            RAISE EXCEPTION 'that role answers for the whole organization and cannot be granted over one project'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    IF NEW.group_id IS NOT NULL THEN
        SELECT org_id INTO subject_org FROM user_group WHERE id = NEW.group_id;
        IF subject_org IS DISTINCT FROM NEW.org_id THEN
            RAISE EXCEPTION 'a role cannot be granted to another organization''s group'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    IF NEW.user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id
    ) THEN
        RAISE EXCEPTION 'a role can only be granted to a member of the organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION saved_filter_share_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.owner_id AND org_role = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a saved filter'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION team_member_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    team_org uuid;
BEGIN
    SELECT org_id INTO team_org FROM team WHERE id = NEW.team_id;
    IF team_org IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a team member cannot cross organizations'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM org_member
         WHERE org_id = NEW.org_id AND user_id = NEW.user_id
           AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member of the organization can join one of its teams'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION team_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    team_project uuid;
    team_org     uuid;
BEGIN
    IF NEW.team_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id, org_id INTO team_project, team_org FROM team WHERE id = NEW.team_id;
    IF team_org IS DISTINCT FROM NEW.org_id OR team_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'that team is not in this project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION theme_share_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.owner_id AND org_role = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a theme'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION theme_unshared_default() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE org SET default_theme_id = NULL WHERE id = NEW.org_id AND default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION workflow_scheme_item_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    scheme_org   uuid;
    workflow_org uuid;
    type_org     uuid;
BEGIN
    SELECT org_id INTO scheme_org   FROM workflow_scheme WHERE id = NEW.scheme_id;
    SELECT org_id INTO workflow_org FROM workflow        WHERE id = NEW.workflow_id;
    IF scheme_org IS DISTINCT FROM NEW.org_id OR workflow_org IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a workflow mapping cannot cross organizations'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.issue_type_id IS NOT NULL THEN
        SELECT org_id INTO type_org FROM issue_type WHERE id = NEW.issue_type_id;
        IF type_org IS DISTINCT FROM NEW.org_id THEN
            RAISE EXCEPTION 'a workflow mapping cannot cross organizations'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION workflow_scheme_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    target_org uuid := COALESCE(NEW.org_id, OLD.org_id);
    default_id uuid;
BEGIN
    -- The organization itself is going away and taking its schemes with it.
    IF NOT EXISTS (SELECT 1 FROM org WHERE id = target_org) THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM workflow_scheme WHERE org_id = target_org) THEN
        RETURN NULL;
    END IF;

    SELECT id INTO default_id FROM workflow_scheme
     WHERE org_id = target_org AND is_default;
    IF default_id IS NULL THEN
        RAISE EXCEPTION 'an organization has to keep one default workflow scheme'
            USING ERRCODE = 'check_violation';
    END IF;

    -- Without a catch-all the default scheme is not a backstop, and an issue
    -- type nobody mapped would have no workflow to start in.
    IF NOT EXISTS (
        SELECT 1 FROM workflow_scheme_item
         WHERE scheme_id = default_id AND issue_type_id IS NULL
    ) THEN
        RAISE EXCEPTION 'the default workflow scheme has to map the issue types it does not name'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER automation_rule_project_in_org BEFORE INSERT OR UPDATE ON automation_rule
    FOR EACH ROW EXECUTE FUNCTION automation_rule_project_in_org();
CREATE TRIGGER dashboard_share_check BEFORE INSERT OR UPDATE OF dashboard_id, org_id ON dashboard_share
    FOR EACH ROW EXECUTE FUNCTION dashboard_share_guard();
CREATE TRIGGER holiday_same_org BEFORE INSERT OR UPDATE ON holiday
    FOR EACH ROW EXECUTE FUNCTION holiday_same_org();
CREATE TRIGGER issue_link_check BEFORE INSERT OR UPDATE ON issue_link
    FOR EACH ROW EXECUTE FUNCTION issue_link_guard();
CREATE TRIGGER issue_old_key_same_org BEFORE INSERT OR UPDATE ON issue_old_key
    FOR EACH ROW EXECUTE FUNCTION issue_old_key_same_org();
CREATE TRIGGER issue_remote_link_same_org BEFORE INSERT OR UPDATE ON issue_remote_link
    FOR EACH ROW EXECUTE FUNCTION issue_remote_link_same_org();
CREATE TRIGGER notification_same_org BEFORE INSERT OR UPDATE ON notification
    FOR EACH ROW EXECUTE FUNCTION notification_same_org();
CREATE TRIGGER project_scheme_check BEFORE INSERT OR UPDATE OF workflow_scheme_id, org_id ON project
    FOR EACH ROW EXECUTE FUNCTION project_scheme_guard();
CREATE TRIGGER workflow_scheme_item_check BEFORE INSERT OR UPDATE ON workflow_scheme_item
    FOR EACH ROW EXECUTE FUNCTION workflow_scheme_item_guard();
