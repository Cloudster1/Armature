-- +goose Up
-- Administrators keep project templates of their own beside the built-in ones
-- (#467). A template is applied once: the project copies what it needs and
-- never refers back, so nothing here is referenced by a project.
CREATE TABLE project_template (
    id                 uuid         PRIMARY KEY DEFAULT uuidv7(),
    name               text         NOT NULL,
    description        text         NOT NULL DEFAULT '',
    kind               project_kind NOT NULL DEFAULT 'software',
    board_type         board_type   NOT NULL DEFAULT 'kanban',
    planning_method    text         NOT NULL DEFAULT 'kanban',
    features           text[]       NOT NULL DEFAULT '{}',
    -- Null follows everyone's default scheme, whichever that is when a project is made.
    workflow_scheme_id uuid         REFERENCES workflow_scheme (id) ON DELETE RESTRICT,
    desk               boolean      NOT NULL DEFAULT false,
    archived_at        timestamptz,
    created_by         uuid         REFERENCES app_user (id) ON DELETE SET NULL,
    created_at         timestamptz  NOT NULL DEFAULT now(),
    updated_at         timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT project_template_name_given CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
    CONSTRAINT project_template_description_short CHECK (char_length(description) <= 1000),
    CONSTRAINT project_template_planning_method_known CHECK (planning_method IN ('scrum', 'kanban')),
    -- The same pages a project may have, and the desk's only on a service project.
    CONSTRAINT project_template_features_known CHECK (
        features <@ '{board,sprints,plan,calendar,milestones,releases,components,hierarchy,dashboard,queues,desk,teams,repositories,automation,import,resources}'::text[]
    ),
    CONSTRAINT project_template_features_desk CHECK (NOT (features && '{queues,desk}'::text[]) OR kind = 'service'),
    CONSTRAINT project_template_desk_is_service CHECK (NOT desk OR kind = 'service')
);
CREATE UNIQUE INDEX project_template_name_idx ON project_template (lower(name));
CREATE INDEX project_template_scheme_idx ON project_template (workflow_scheme_id);

-- The issue types a project made from the template allows, in its order.
CREATE TABLE project_template_issue_type (
    template_id   uuid NOT NULL REFERENCES project_template (id) ON DELETE CASCADE,
    issue_type_id uuid NOT NULL REFERENCES issue_type (id) ON DELETE RESTRICT,
    position      int  NOT NULL DEFAULT 0,
    PRIMARY KEY (template_id, issue_type_id)
);
CREATE INDEX project_template_issue_type_type_idx ON project_template_issue_type (issue_type_id);

-- A retired type goes into no template, as into no project.
CREATE TRIGGER project_template_issue_type_not_retired
    BEFORE INSERT OR UPDATE OF issue_type_id ON project_template_issue_type
    FOR EACH ROW EXECUTE FUNCTION issue_type_not_retired();

-- A template gives its projects a standard type that is not retired, as every
-- project must have. Checked at commit, so a template and its types are written together.
-- +goose StatementBegin
CREATE FUNCTION project_template_keeps_a_standard_type() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    lacking text;
BEGIN
    IF TG_TABLE_NAME = 'project_template' THEN
        SELECT pt.name INTO lacking FROM project_template pt
        WHERE pt.id = NEW.id
          AND NOT EXISTS (
              SELECT 1 FROM project_template_issue_type ptt JOIN issue_type t ON t.id = ptt.issue_type_id
              WHERE ptt.template_id = pt.id AND t.hierarchy_level = 0 AND t.retired_at IS NULL);
    ELSIF TG_TABLE_NAME = 'project_template_issue_type' THEN
        SELECT pt.name INTO lacking FROM project_template pt
        WHERE pt.id IN (OLD.template_id, NEW.template_id)
          AND NOT EXISTS (
              SELECT 1 FROM project_template_issue_type ptt JOIN issue_type t ON t.id = ptt.issue_type_id
              WHERE ptt.template_id = pt.id AND t.hierarchy_level = 0 AND t.retired_at IS NULL)
        LIMIT 1;
    ELSE
        SELECT pt.name INTO lacking FROM project_template pt
        WHERE EXISTS (SELECT 1 FROM project_template_issue_type WHERE template_id = pt.id AND issue_type_id = NEW.id)
          AND NOT EXISTS (
              SELECT 1 FROM project_template_issue_type ptt JOIN issue_type t ON t.id = ptt.issue_type_id
              WHERE ptt.template_id = pt.id AND t.hierarchy_level = 0 AND t.retired_at IS NULL)
        LIMIT 1;
    END IF;
    IF lacking IS NOT NULL THEN
        RAISE EXCEPTION 'the project template % would be left without a standard issue type that is not retired', lacking
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_template_keeps_a_standard_type';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER project_template_keeps_a_standard_type
    AFTER INSERT ON project_template
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION project_template_keeps_a_standard_type();
CREATE CONSTRAINT TRIGGER project_template_type_keeps_a_standard_type
    AFTER INSERT OR UPDATE OR DELETE ON project_template_issue_type
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION project_template_keeps_a_standard_type();
CREATE CONSTRAINT TRIGGER issue_type_keeps_templates_a_standard_type
    AFTER UPDATE OF retired_at, hierarchy_level ON issue_type
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION project_template_keeps_a_standard_type();

-- +goose Down
DROP TRIGGER IF EXISTS issue_type_keeps_templates_a_standard_type ON issue_type;
DROP TABLE IF EXISTS project_template_issue_type;
DROP TABLE IF EXISTS project_template;
DROP FUNCTION IF EXISTS project_template_keeps_a_standard_type();
