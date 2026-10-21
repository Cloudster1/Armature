-- +goose Up
-- The suite demo (#300): the issues of Armature, Exciter and Stator from a
-- snapshot, built by the worker. One row says how far it got.

CREATE TABLE suite_demo (
    -- One installation has one suite demo.
    id         boolean PRIMARY KEY DEFAULT true CHECK (id),
    state      text NOT NULL DEFAULT 'queued',
    step       integer NOT NULL DEFAULT 0,
    steps      integer NOT NULL,
    message    text NOT NULL DEFAULT '',
    taken      text NOT NULL,
    actor_id   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT suite_demo_state_known CHECK (state IN ('queued', 'running', 'done', 'failed')),
    CONSTRAINT suite_demo_step_shape CHECK (step BETWEEN 0 AND steps)
);

CREATE TRIGGER suite_demo_set_updated_at BEFORE UPDATE ON suite_demo
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON suite_demo TO armature_app, armature_admin;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS suite_demo;
