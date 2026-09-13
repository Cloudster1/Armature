-- +goose Up
-- A project has the pages its template gave it and its administrators kept.
-- The names are the route slugs, so the sidebar and the guard agree.
ALTER TABLE project ADD COLUMN features text[] NOT NULL DEFAULT '{}';

UPDATE project SET features = CASE kind
    WHEN 'service' THEN '{board,calendar,dashboard,queues,desk,teams,automation,import}'::text[]
    ELSE '{board,sprints,plan,calendar,milestones,releases,components,hierarchy,dashboard,teams,repositories,automation,import}'::text[]
END;

ALTER TABLE project ADD CONSTRAINT project_features_known CHECK (
    features <@ '{board,sprints,plan,calendar,milestones,releases,components,hierarchy,dashboard,queues,desk,teams,repositories,automation,import}'::text[]
);
ALTER TABLE project ADD CONSTRAINT project_features_desk CHECK (
    NOT (features && '{queues,desk}'::text[]) OR kind = 'service'
);

-- A row for a page the project does not have is refused whatever the
-- service believed. The feature's name is the trigger's argument.
-- +goose StatementBegin
CREATE FUNCTION project_feature_on() RETURNS trigger AS $$
BEGIN
    IF NEW.project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM project WHERE id = NEW.project_id AND TG_ARGV[0] = ANY (features)
    ) THEN
        RAISE EXCEPTION 'this project does not use %', TG_ARGV[0]
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER sprint_feature_on BEFORE INSERT ON sprint
    FOR EACH ROW EXECUTE FUNCTION project_feature_on('sprints');
CREATE TRIGGER milestone_feature_on BEFORE INSERT ON milestone
    FOR EACH ROW EXECUTE FUNCTION project_feature_on('milestones');
CREATE TRIGGER version_feature_on BEFORE INSERT ON version
    FOR EACH ROW EXECUTE FUNCTION project_feature_on('releases');
CREATE TRIGGER component_feature_on BEFORE INSERT ON component
    FOR EACH ROW EXECUTE FUNCTION project_feature_on('components');
CREATE TRIGGER team_feature_on BEFORE INSERT ON team
    FOR EACH ROW EXECUTE FUNCTION project_feature_on('teams');
CREATE TRIGGER git_repository_feature_on BEFORE INSERT ON git_repository
    FOR EACH ROW EXECUTE FUNCTION project_feature_on('repositories');
CREATE TRIGGER automation_rule_feature_on BEFORE INSERT ON automation_rule
    FOR EACH ROW EXECUTE FUNCTION project_feature_on('automation');
CREATE TRIGGER import_job_feature_on BEFORE INSERT ON import_job
    FOR EACH ROW EXECUTE FUNCTION project_feature_on('import');

-- +goose Down
DROP TRIGGER IF EXISTS import_job_feature_on ON import_job;
DROP TRIGGER IF EXISTS automation_rule_feature_on ON automation_rule;
DROP TRIGGER IF EXISTS git_repository_feature_on ON git_repository;
DROP TRIGGER IF EXISTS team_feature_on ON team;
DROP TRIGGER IF EXISTS component_feature_on ON component;
DROP TRIGGER IF EXISTS version_feature_on ON version;
DROP TRIGGER IF EXISTS milestone_feature_on ON milestone;
DROP TRIGGER IF EXISTS sprint_feature_on ON sprint;
DROP FUNCTION IF EXISTS project_feature_on();
ALTER TABLE project DROP CONSTRAINT IF EXISTS project_features_desk;
ALTER TABLE project DROP CONSTRAINT IF EXISTS project_features_known;
ALTER TABLE project DROP COLUMN IF EXISTS features;
