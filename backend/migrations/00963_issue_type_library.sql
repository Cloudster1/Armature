-- +goose Up
-- Administrators keep a library of issue types (#464). The types the
-- installation started with are built in: code finds them by name, so they keep
-- their name and level, and are never retired or deleted.
ALTER TABLE issue_type ADD COLUMN builtin boolean NOT NULL DEFAULT false;
UPDATE issue_type SET builtin = true;

-- A retired type stays on the issues that have it and is offered for nothing new.
ALTER TABLE issue_type ADD COLUMN retired_at timestamptz;

-- The glyphs the web draws a type with; anything else used to be drawn as a task.
UPDATE issue_type SET icon = 'task'
WHERE icon NOT IN ('task', 'bug', 'story', 'epic', 'subtask', 'initiative',
                   'change', 'improvement', 'incident', 'question', 'research', 'release');
ALTER TABLE issue_type ADD CONSTRAINT issue_type_icon_known
    CHECK (icon IN ('task', 'bug', 'story', 'epic', 'subtask', 'initiative',
                    'change', 'improvement', 'incident', 'question', 'research', 'release'));

-- +goose StatementBegin
CREATE FUNCTION issue_type_builtin_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.builtin THEN
            RAISE EXCEPTION 'the % type is built in and cannot be deleted', OLD.name
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.builtin IS DISTINCT FROM OLD.builtin THEN
        RAISE EXCEPTION 'whether the % type is built in is fixed', OLD.name
            USING ERRCODE = 'check_violation';
    END IF;
    IF OLD.builtin AND (NEW.name IS DISTINCT FROM OLD.name OR NEW.hierarchy_level IS DISTINCT FROM OLD.hierarchy_level) THEN
        RAISE EXCEPTION 'the % type is built in, so its name and level stay', OLD.name
            USING ERRCODE = 'check_violation';
    END IF;
    IF OLD.builtin AND NEW.retired_at IS NOT NULL THEN
        RAISE EXCEPTION 'the % type is built in and cannot be retired', OLD.name
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER issue_type_builtin_check
    BEFORE UPDATE OR DELETE ON issue_type
    FOR EACH ROW EXECUTE FUNCTION issue_type_builtin_guard();

-- +goose Down
DROP TRIGGER IF EXISTS issue_type_builtin_check ON issue_type;
DROP FUNCTION IF EXISTS issue_type_builtin_guard();
ALTER TABLE issue_type DROP CONSTRAINT IF EXISTS issue_type_icon_known;
ALTER TABLE issue_type DROP COLUMN IF EXISTS retired_at;
ALTER TABLE issue_type DROP COLUMN IF EXISTS builtin;
