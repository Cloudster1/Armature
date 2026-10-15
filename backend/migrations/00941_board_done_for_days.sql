-- +goose Up
-- How many days a kanban board keeps an issue after it is finished (#54), so
-- its done lane stops growing forever. 0 keeps finished work on the board.
ALTER TABLE board
    ADD COLUMN done_for_days integer NOT NULL DEFAULT 14,
    ADD CONSTRAINT board_done_for_days_not_negative CHECK (done_for_days >= 0);

-- +goose Down
ALTER TABLE board
    DROP CONSTRAINT IF EXISTS board_done_for_days_not_negative,
    DROP COLUMN IF EXISTS done_for_days;
