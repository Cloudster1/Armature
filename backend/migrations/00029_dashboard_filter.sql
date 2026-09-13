-- +goose Up
-- A dashboard narrows with one filter tile. The service refuses a second; the
-- index is what makes the refusal true whatever path a row takes.
CREATE UNIQUE INDEX dashboard_widget_one_filter_idx ON dashboard_widget (dashboard_id) WHERE kind = 'filter';

-- +goose Down
DROP INDEX IF EXISTS dashboard_widget_one_filter_idx;
