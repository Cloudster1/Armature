-- +goose Up
-- Projects created before boards existed need one, or their board page is a
-- dead end. New projects get theirs in the transaction that creates them.

-- +goose StatementBegin
DO $$
DECLARE
    p            record;
    new_board_id uuid;
    st           record;
    lane_id      uuid;
    lane_pos     integer;
BEGIN
    FOR p IN SELECT id, org_id, name, workflow_scheme_id FROM project
             WHERE NOT EXISTS (SELECT 1 FROM board b WHERE b.project_id = project.id)
    LOOP
        INSERT INTO board (org_id, project_id, name, description)
        VALUES (p.org_id, p.id, p.name || ' board',
                'Cards are grouped by the states that make up each swimlane.')
        RETURNING id INTO new_board_id;

        lane_pos := 0;
        -- One swimlane per distinct status of the workflows this project's
        -- scheme maps, in workflow order.
        FOR st IN
            SELECT DISTINCT ON (s.id) s.id, s.name, step.position
            FROM workflow_scheme_item i
            JOIN workflow_step step ON step.workflow_id = i.workflow_id
            JOIN issue_status s ON s.id = step.status_id
            WHERE i.scheme_id = p.workflow_scheme_id
            ORDER BY s.id, step.position
        LOOP
            INSERT INTO board_swimlane (org_id, board_id, name, position)
            VALUES (p.org_id, new_board_id, st.name, lane_pos)
            RETURNING id INTO lane_id;

            INSERT INTO board_swimlane_status (org_id, board_id, swimlane_id, status_id)
            VALUES (p.org_id, new_board_id, lane_id, st.id);

            lane_pos := lane_pos + 1;
        END LOOP;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- The loop above orders swimlanes by status id rather than by workflow
-- position, because DISTINCT ON requires it. Put them back into workflow order.
-- +goose StatementBegin
DO $$
DECLARE
    l record;
BEGIN
    FOR l IN
        SELECT sl.id, row_number() OVER (PARTITION BY sl.board_id ORDER BY s.position, s.name) - 1 AS correct
        FROM board_swimlane sl
        JOIN board_swimlane_status ls ON ls.swimlane_id = sl.id
        JOIN issue_status s ON s.id = ls.status_id
    LOOP
        UPDATE board_swimlane SET position = l.correct WHERE id = l.id;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Boards created by this migration are indistinguishable from ones made since,
-- so there is nothing safe to undo here.
SELECT 1;
