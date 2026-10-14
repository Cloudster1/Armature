-- +goose Up
-- The global administrator administers the installation, in the words the rest
-- of the product uses (#218). A description somebody has rewritten is theirs.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION org_builtin_roles(target uuid) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO org_role (org_id, key, name, description, org_wide_only, builtin, permissions, sort_order) VALUES
        (target, 'global_administrator', 'Global administrator',
         'Administers the installation: its members, its roles, its workflows and every project in it.',
         true, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                           'board.configure', 'project.administer', 'project.create', 'org.administer'], 10),
        (target, 'project_administrator', 'Project administrator',
         'Configures one project: its boards, its workflow scheme, its teams, and whether it is archived.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                            'board.configure', 'project.administer'], 20),
        (target, 'scrum_master', 'Scrum master',
         'Runs the work in one project: sprints, teams and the planning that goes with them.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage'], 30),
        (target, 'user', 'User',
         'Does the ordinary work: files issues, moves them, comments.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write'], 40),
        (target, 'reader', 'Reader',
         'Sees a project and changes nothing in it.',
         false, true, ARRAY['read'], 50)
    ON CONFLICT (key) DO NOTHING;
END;
$$;
-- +goose StatementEnd

UPDATE org_role SET description = 'Administers the installation: its members, its roles, its workflows and every project in it.'
WHERE key = 'global_administrator' AND builtin AND description = 'Administers the tenant: its members, its roles, its workflows and every project in it.';

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION org_builtin_roles(target uuid) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO org_role (org_id, key, name, description, org_wide_only, builtin, permissions, sort_order) VALUES
        (target, 'global_administrator', 'Global administrator',
         'Administers the tenant: its members, its roles, its workflows and every project in it.',
         true, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                           'board.configure', 'project.administer', 'project.create', 'org.administer'], 10),
        (target, 'project_administrator', 'Project administrator',
         'Configures one project: its boards, its workflow scheme, its teams, and whether it is archived.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                            'board.configure', 'project.administer'], 20),
        (target, 'scrum_master', 'Scrum master',
         'Runs the work in one project: sprints, teams and the planning that goes with them.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage'], 30),
        (target, 'user', 'User',
         'Does the ordinary work: files issues, moves them, comments.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write'], 40),
        (target, 'reader', 'Reader',
         'Sees a project and changes nothing in it.',
         false, true, ARRAY['read'], 50)
    ON CONFLICT (key) DO NOTHING;
END;
$$;
-- +goose StatementEnd

UPDATE org_role SET description = 'Administers the tenant: its members, its roles, its workflows and every project in it.'
WHERE key = 'global_administrator' AND builtin AND description = 'Administers the installation: its members, its roles, its workflows and every project in it.';
