-- +goose Up
-- Teams: the people inside a project who work together, and the boards,
-- backlogs and sprints that belong to them rather than to the project at large.

CREATE TABLE team (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    position    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT team_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX team_project_name_idx ON team (project_id, lower(name));
CREATE INDEX team_project_idx ON team (project_id, position);

CREATE TRIGGER team_set_updated_at BEFORE UPDATE ON team
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A team is made of people who are already in the organization. Membership of a
-- team is narrower than membership of the organization, never wider: joining a
-- team is not a way to gain access to anything.
CREATE TABLE team_member (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    team_id    uuid NOT NULL REFERENCES team(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    -- A lead is a person to ask, not a permission. Nothing is refused on it.
    is_lead    boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);

CREATE INDEX team_member_user_idx ON team_member (org_id, user_id);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION team_member_guard() RETURNS trigger AS $$
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
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER team_member_check
    BEFORE INSERT OR UPDATE ON team_member
    FOR EACH ROW EXECUTE FUNCTION team_member_guard();

-- ------------------------------------------------ what belongs to a team ----

-- Null is work the project has not handed to anyone in particular, which is
-- where everything starts and where a lot of it stays.
ALTER TABLE issue ADD COLUMN team_id uuid REFERENCES team(id) ON DELETE SET NULL;
CREATE INDEX issue_team_idx ON issue (team_id) WHERE team_id IS NOT NULL;

-- A board scoped to a team shows that team's work and nothing else. Null is a
-- board over the whole project, which is what every project starts with.
ALTER TABLE board ADD COLUMN team_id uuid REFERENCES team(id) ON DELETE SET NULL;
CREATE INDEX board_team_idx ON board (team_id) WHERE team_id IS NOT NULL;

-- A sprint belongs to whoever is running it. Two teams in one project run their
-- own sprints, which is the whole point of giving them their own backlogs.
ALTER TABLE sprint ADD COLUMN team_id uuid REFERENCES team(id) ON DELETE SET NULL;
CREATE INDEX sprint_team_idx ON sprint (team_id) WHERE team_id IS NOT NULL;

-- One running sprint per team, and one for the project's own unassigned work.
-- NULLS NOT DISTINCT is what makes the second half true: without it two
-- project-wide sprints could run at once.
DROP INDEX sprint_one_active_idx;
CREATE UNIQUE INDEX sprint_one_active_idx ON sprint (project_id, team_id)
    NULLS NOT DISTINCT WHERE state = 'active';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION team_scope_guard() RETURNS trigger AS $$
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
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_team_check
    BEFORE INSERT OR UPDATE OF team_id, project_id, org_id ON issue
    FOR EACH ROW EXECUTE FUNCTION team_scope_guard();

CREATE TRIGGER board_team_check
    BEFORE INSERT OR UPDATE OF team_id, project_id, org_id ON board
    FOR EACH ROW EXECUTE FUNCTION team_scope_guard();

CREATE TRIGGER sprint_team_check
    BEFORE INSERT OR UPDATE OF team_id, project_id, org_id ON sprint
    FOR EACH ROW EXECUTE FUNCTION team_scope_guard();

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['team', 'team_member']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id())',
            t || '_tenant_isolation', t);

        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
            EXECUTE format(
                'CREATE POLICY %I ON %I TO armature_admin USING (true) WITH CHECK (true)',
                t || '_admin_bypass', t);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON team, team_member TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS sprint_team_check ON sprint;
DROP TRIGGER IF EXISTS board_team_check ON board;
DROP TRIGGER IF EXISTS issue_team_check ON issue;
DROP FUNCTION IF EXISTS team_scope_guard();

DROP INDEX IF EXISTS sprint_one_active_idx;
DROP INDEX IF EXISTS sprint_team_idx;
ALTER TABLE sprint DROP COLUMN IF EXISTS team_id;
CREATE UNIQUE INDEX sprint_one_active_idx ON sprint (project_id) WHERE state = 'active';

DROP INDEX IF EXISTS board_team_idx;
ALTER TABLE board DROP COLUMN IF EXISTS team_id;
DROP INDEX IF EXISTS issue_team_idx;
ALTER TABLE issue DROP COLUMN IF EXISTS team_id;

DROP TRIGGER IF EXISTS team_member_check ON team_member;
DROP FUNCTION IF EXISTS team_member_guard();
DROP TABLE IF EXISTS team_member, team CASCADE;
