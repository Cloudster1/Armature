-- +goose Up
-- Where the designer drew each status. Both null means the status has never
-- been placed and the designer lays it out itself; one without the other would
-- be a point nobody can draw.
ALTER TABLE workflow_step ADD COLUMN layout_x integer;
ALTER TABLE workflow_step ADD COLUMN layout_y integer;
ALTER TABLE workflow_step ADD CONSTRAINT workflow_step_layout_is_a_point
    CHECK ((layout_x IS NULL) = (layout_y IS NULL));

-- +goose Down
ALTER TABLE workflow_step DROP CONSTRAINT IF EXISTS workflow_step_layout_is_a_point;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS layout_y;
ALTER TABLE workflow_step DROP COLUMN IF EXISTS layout_x;
