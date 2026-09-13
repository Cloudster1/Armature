-- +goose Up
-- Boards: a view of a project's issues arranged into swimlanes, where each
-- swimlane is defined by the ticket states that belong to it.

-- ------------------------------------------------------------- ordering ----

-- Cards need an order that survives being dragged anywhere. Integer positions
-- would mean renumbering every card below an insertion, which is a write per
-- card and a lost update away from two cards claiming one slot. A
-- lexicographic rank is a string whose ordinary comparison is its position, so
-- a move is one row update.
ALTER TABLE issue ADD COLUMN rank text;

-- Existing issues are ranked by age. Fixed width hexadecimal sorts in the same
-- order as the number it encodes, and the trailing character keeps the value
-- clear of the "must not end in the lowest digit" rule the rank generator
-- relies on.
-- +goose StatementBegin
DO $$
BEGIN
    UPDATE issue i
    SET rank = ranked.generated
    FROM (
        SELECT id, lpad(to_hex(row_number() OVER (ORDER BY created_at, id)), 10, '0') || 'V' AS generated
        FROM issue
    ) AS ranked
    WHERE i.id = ranked.id;
END;
$$;
-- +goose StatementEnd

ALTER TABLE issue ALTER COLUMN rank SET NOT NULL;

CREATE INDEX issue_rank_idx ON issue (org_id, project_id, rank);

-- --------------------------------------------------------------- boards ----

-- Rows within a swimlane can be grouped, which is the other axis a board is
-- usually read along: who is working on it, or how urgent it is.
CREATE TYPE board_grouping AS ENUM ('none', 'assignee', 'priority', 'type');

CREATE TABLE board (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    group_by    board_grouping NOT NULL DEFAULT 'none',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT board_name_not_blank CHECK (btrim(name) <> '')
);

CREATE INDEX board_project_idx ON board (project_id);
CREATE UNIQUE INDEX board_project_name_idx ON board (project_id, lower(name));

CREATE TRIGGER board_set_updated_at BEFORE UPDATE ON board
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A swimlane is a named group of statuses. Which statuses belong to it is the
-- whole of its configuration: a card is in the swimlane that claims its status.
CREATE TABLE board_swimlane (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    board_id   uuid NOT NULL REFERENCES board(id) ON DELETE CASCADE,
    name       text NOT NULL,
    position   integer NOT NULL DEFAULT 0,
    -- A soft limit on how many cards should sit here at once. Zero means none.
    -- It is deliberately not enforced: a work in progress limit is a signal to
    -- the team, and a board that refuses to accept reality is worse than one
    -- that shows it.
    wip_limit  integer NOT NULL DEFAULT 0 CHECK (wip_limit >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT board_swimlane_name_not_blank CHECK (btrim(name) <> '')
);

CREATE INDEX board_swimlane_board_idx ON board_swimlane (board_id, position);

CREATE TRIGGER board_swimlane_set_updated_at BEFORE UPDATE ON board_swimlane
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE board_swimlane_status (
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    board_id    uuid NOT NULL REFERENCES board(id) ON DELETE CASCADE,
    swimlane_id uuid NOT NULL REFERENCES board_swimlane(id) ON DELETE CASCADE,
    status_id   uuid NOT NULL REFERENCES issue_status(id) ON DELETE CASCADE,
    PRIMARY KEY (swimlane_id, status_id),
    -- A status belongs to at most one swimlane on a board. Without this a card
    -- could be in two places at once and a move would have no single answer.
    UNIQUE (board_id, status_id)
);

CREATE INDEX board_swimlane_status_board_idx ON board_swimlane_status (board_id);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['board', 'board_swimlane', 'board_swimlane_status']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id())',
            t || '_tenant_isolation', t);

        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
            EXECUTE format(
                'CREATE POLICY %I ON %I TO armature_admin USING (true) WITH CHECK (true)',
                t || '_admin_bypass', t);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO armature_app, armature_admin;
        GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS board_swimlane_status, board_swimlane, board CASCADE;
DROP TYPE IF EXISTS board_grouping;
DROP INDEX IF EXISTS issue_rank_idx;
ALTER TABLE issue DROP COLUMN IF EXISTS rank;
