-- +goose Up
-- Boards have a type, and projects remember the template they were made from.

-- A scrum board shows the sprint that is running and nothing else; a kanban
-- board shows everything in its scope and leans on its swimlane limits instead.
-- The type is what decides which cards a board draws, so it is a column and not
-- a label.
CREATE TYPE board_type AS ENUM ('scrum', 'kanban');

-- Every board so far has shown all of its work regardless of sprints, which is
-- what a kanban board does. Backfilling to kanban changes nothing anybody sees.
ALTER TABLE board ADD COLUMN type board_type NOT NULL DEFAULT 'kanban';

-- The template a project was set up from, empty for one made before templates
-- existed or without one. It is a record of how the project began, not a live
-- link: changing what a template means does not reach into projects made
-- from it.
ALTER TABLE project ADD COLUMN template text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE project DROP COLUMN IF EXISTS template;
ALTER TABLE board DROP COLUMN IF EXISTS type;
DROP TYPE IF EXISTS board_type;
