-- +goose Up
-- Planning: issues gain a start date, so an issue occupies a stretch of time
-- rather than a single deadline.

-- A due date alone answers "when must this be finished" and nothing about when
-- the work happens. A timeline needs both ends.
ALTER TABLE issue ADD COLUMN start_date date;

-- Either end may be missing while a plan is being sketched out; only a range
-- that ends before it starts is nonsense.
ALTER TABLE issue ADD CONSTRAINT issue_range_not_backwards
    CHECK (start_date IS NULL OR due_date IS NULL OR start_date <= due_date);

-- The plan reads a project's scheduled issues in date order.
CREATE INDEX issue_schedule_idx ON issue (project_id, start_date, due_date)
    WHERE start_date IS NOT NULL OR due_date IS NOT NULL;

-- Existing demo and test data has no dates, which is the correct starting
-- point: an unscheduled issue is shown as unscheduled rather than guessed at.

-- ---------------------------------------------------------- dependencies ----

-- issue_link has existed since the first migration with nothing writing to it.
-- The plan is what makes it worth having, so it gets the index a dependency
-- lookup needs and the guarantee that a link is inside one organization.
CREATE INDEX issue_link_type_source_idx ON issue_link (org_id, link_type_id, source_id);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_link_guard() RETURNS trigger AS $$
DECLARE
    source_org uuid;
    target_org uuid;
BEGIN
    SELECT org_id INTO source_org FROM issue WHERE id = NEW.source_id;
    SELECT org_id INTO target_org FROM issue WHERE id = NEW.target_id;

    IF source_org IS NULL OR target_org IS NULL THEN
        RAISE EXCEPTION 'both ends of a link have to exist'
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    IF source_org <> target_org OR source_org <> NEW.org_id THEN
        RAISE EXCEPTION 'a link cannot cross organizations'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_link_check
    BEFORE INSERT OR UPDATE ON issue_link
    FOR EACH ROW EXECUTE FUNCTION issue_link_guard();

-- Two issues may be related in several ways, but not twice the same way in the
-- same direction, and "A blocks B" already says everything "B blocks A" would.
CREATE UNIQUE INDEX issue_link_unordered_idx ON issue_link (
    link_type_id,
    LEAST(source_id, target_id),
    GREATEST(source_id, target_id)
);

-- +goose Down
DROP INDEX IF EXISTS issue_link_unordered_idx;
DROP TRIGGER IF EXISTS issue_link_check ON issue_link;
DROP FUNCTION IF EXISTS issue_link_guard();
DROP INDEX IF EXISTS issue_link_type_source_idx;
DROP INDEX IF EXISTS issue_schedule_idx;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_range_not_backwards;
ALTER TABLE issue DROP COLUMN IF EXISTS start_date;
