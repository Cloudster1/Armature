-- +goose Up
-- An event remembers the trace of the request that emitted it, so the work the
-- worker does for it is drawn in the same trace rather than as a root of its own.
ALTER TABLE outbox_event ADD COLUMN trace_parent text;

-- +goose Down
ALTER TABLE outbox_event DROP COLUMN IF EXISTS trace_parent;
