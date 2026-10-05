-- +goose Up
-- How a project plans its people. A scrum project plans by team, since a
-- sprint is a team's commitment; a kanban project plans by team or by person.
ALTER TABLE project
    ADD COLUMN planning_method text,
    ADD COLUMN resource_grouping text NOT NULL DEFAULT 'team';

-- A project with a scrum board runs sprints; everything else flows.
UPDATE project p SET planning_method = CASE
    WHEN EXISTS (SELECT 1 FROM board b WHERE b.project_id = p.id AND b.type = 'scrum') THEN 'scrum'
    ELSE 'kanban'
END;

ALTER TABLE project
    ALTER COLUMN planning_method SET NOT NULL,
    ALTER COLUMN planning_method SET DEFAULT 'kanban',
    ADD CONSTRAINT project_planning_method_known CHECK (planning_method IN ('scrum', 'kanban')),
    ADD CONSTRAINT project_resource_grouping_known CHECK (resource_grouping IN ('team', 'person')),
    -- The service resets the grouping when a project turns to scrum; a row
    -- written any other way is held to the same rule.
    ADD CONSTRAINT project_scrum_plans_by_team CHECK (planning_method = 'kanban' OR resource_grouping = 'team');

-- The resource view is a page like the others, on by default where the plan is.
ALTER TABLE project DROP CONSTRAINT project_features_known;
ALTER TABLE project ADD CONSTRAINT project_features_known CHECK (
    features <@ '{board,sprints,plan,calendar,milestones,releases,components,hierarchy,dashboard,queues,desk,teams,repositories,automation,import,resources}'::text[]
);
UPDATE project SET features = array_append(features, 'resources')
WHERE kind = 'software' AND NOT 'resources' = ANY (features);

-- +goose Down
UPDATE project SET features = array_remove(features, 'resources');
ALTER TABLE project DROP CONSTRAINT project_features_known;
ALTER TABLE project ADD CONSTRAINT project_features_known CHECK (
    features <@ '{board,sprints,plan,calendar,milestones,releases,components,hierarchy,dashboard,queues,desk,teams,repositories,automation,import}'::text[]
);
ALTER TABLE project
    DROP CONSTRAINT IF EXISTS project_scrum_plans_by_team,
    DROP CONSTRAINT IF EXISTS project_resource_grouping_known,
    DROP CONSTRAINT IF EXISTS project_planning_method_known,
    DROP COLUMN IF EXISTS resource_grouping,
    DROP COLUMN IF EXISTS planning_method;
