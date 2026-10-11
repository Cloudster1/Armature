-- +goose Up
-- A team's sprints and the request types routed to it no longer fall to the
-- project when the team is deleted: the delete is refused until somebody has
-- decided where they go. NO ACTION rather than RESTRICT, because it is checked
-- once the statement is done, so an organization or project deleted with
-- everything in it still takes its teams and their sprints along.
ALTER TABLE sprint DROP CONSTRAINT sprint_team_id_fkey;
ALTER TABLE sprint
    ADD CONSTRAINT sprint_team_id_fkey
    FOREIGN KEY (team_id) REFERENCES team(id) ON DELETE NO ACTION;

ALTER TABLE request_type DROP CONSTRAINT request_type_team_id_fkey;
ALTER TABLE request_type
    ADD CONSTRAINT request_type_team_id_fkey
    FOREIGN KEY (team_id) REFERENCES team(id) ON DELETE NO ACTION;

-- +goose Down
ALTER TABLE request_type DROP CONSTRAINT request_type_team_id_fkey;
ALTER TABLE request_type
    ADD CONSTRAINT request_type_team_id_fkey
    FOREIGN KEY (team_id) REFERENCES team(id) ON DELETE SET NULL;

ALTER TABLE sprint DROP CONSTRAINT sprint_team_id_fkey;
ALTER TABLE sprint
    ADD CONSTRAINT sprint_team_id_fkey
    FOREIGN KEY (team_id) REFERENCES team(id) ON DELETE SET NULL;
