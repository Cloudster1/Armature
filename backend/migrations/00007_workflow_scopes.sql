-- +goose Up
-- Workflows get two levels. The organization keeps one scheme that answers for
-- every project, and a project may name its own to disagree.

-- ------------------------------------------------- the tenant's default ----

-- One scheme per organization is the one every project falls back to. The
-- partial unique index allows at most one; the guard below insists on at least
-- one, so "which workflow does this issue type use" always has an answer.
ALTER TABLE workflow_scheme ADD COLUMN is_default boolean NOT NULL DEFAULT false;

CREATE UNIQUE INDEX workflow_scheme_one_default_idx ON workflow_scheme (org_id)
    WHERE is_default;

-- The oldest scheme is the one bootstrap made and every project points at.
UPDATE workflow_scheme s SET is_default = true
 WHERE s.id = (SELECT id FROM workflow_scheme older
                WHERE older.org_id = s.org_id
                ORDER BY created_at, id LIMIT 1);

-- --------------------------------------------- a project's own override ----

-- Null means the project has no opinion and uses the organization's scheme.
-- Every project had to name a scheme before, which made "the same as everyone
-- else" and "deliberately different" indistinguishable.
ALTER TABLE project ALTER COLUMN workflow_scheme_id DROP NOT NULL;

UPDATE project p SET workflow_scheme_id = NULL
  FROM workflow_scheme s
 WHERE s.id = p.workflow_scheme_id AND s.is_default;

CREATE INDEX project_workflow_scheme_idx ON project (workflow_scheme_id)
    WHERE workflow_scheme_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION workflow_scheme_scope_guard() RETURNS trigger AS $$
DECLARE
    target_org uuid := COALESCE(NEW.org_id, OLD.org_id);
    default_id uuid;
BEGIN
    -- The organization itself is going away and taking its schemes with it.
    IF NOT EXISTS (SELECT 1 FROM org WHERE id = target_org) THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM workflow_scheme WHERE org_id = target_org) THEN
        RETURN NULL;
    END IF;

    SELECT id INTO default_id FROM workflow_scheme
     WHERE org_id = target_org AND is_default;
    IF default_id IS NULL THEN
        RAISE EXCEPTION 'an organization has to keep one default workflow scheme'
            USING ERRCODE = 'check_violation';
    END IF;

    -- Without a catch-all the default scheme is not a backstop, and an issue
    -- type nobody mapped would have no workflow to start in.
    IF NOT EXISTS (
        SELECT 1 FROM workflow_scheme_item
         WHERE scheme_id = default_id AND issue_type_id IS NULL
    ) THEN
        RAISE EXCEPTION 'the default workflow scheme has to map the issue types it does not name'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Deferred, so that a transaction may build a scheme and promote it in
-- whichever order suits it and still be judged on the result.
CREATE CONSTRAINT TRIGGER workflow_scheme_scope_check
    AFTER INSERT OR UPDATE OR DELETE ON workflow_scheme
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION workflow_scheme_scope_guard();

CREATE CONSTRAINT TRIGGER workflow_scheme_item_scope_check
    AFTER INSERT OR UPDATE OR DELETE ON workflow_scheme_item
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION workflow_scheme_scope_guard();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION project_scheme_guard() RETURNS trigger AS $$
DECLARE
    scheme_org uuid;
BEGIN
    IF NEW.workflow_scheme_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT org_id INTO scheme_org FROM workflow_scheme WHERE id = NEW.workflow_scheme_id;
    IF scheme_org IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a project cannot borrow another organization''s workflow scheme'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER project_scheme_check
    BEFORE INSERT OR UPDATE OF workflow_scheme_id, org_id ON project
    FOR EACH ROW EXECUTE FUNCTION project_scheme_guard();

-- A scheme item may not point at another organization's workflow either.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION workflow_scheme_item_guard() RETURNS trigger AS $$
DECLARE
    scheme_org   uuid;
    workflow_org uuid;
    type_org     uuid;
BEGIN
    SELECT org_id INTO scheme_org   FROM workflow_scheme WHERE id = NEW.scheme_id;
    SELECT org_id INTO workflow_org FROM workflow        WHERE id = NEW.workflow_id;
    IF scheme_org IS DISTINCT FROM NEW.org_id OR workflow_org IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a workflow mapping cannot cross organizations'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.issue_type_id IS NOT NULL THEN
        SELECT org_id INTO type_org FROM issue_type WHERE id = NEW.issue_type_id;
        IF type_org IS DISTINCT FROM NEW.org_id THEN
            RAISE EXCEPTION 'a workflow mapping cannot cross organizations'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER workflow_scheme_item_check
    BEFORE INSERT OR UPDATE ON workflow_scheme_item
    FOR EACH ROW EXECUTE FUNCTION workflow_scheme_item_guard();

-- +goose Down
DROP TRIGGER IF EXISTS workflow_scheme_item_check ON workflow_scheme_item;
DROP FUNCTION IF EXISTS workflow_scheme_item_guard();
DROP TRIGGER IF EXISTS project_scheme_check ON project;
DROP FUNCTION IF EXISTS project_scheme_guard();
DROP TRIGGER IF EXISTS workflow_scheme_item_scope_check ON workflow_scheme_item;
DROP TRIGGER IF EXISTS workflow_scheme_scope_check ON workflow_scheme;
DROP FUNCTION IF EXISTS workflow_scheme_scope_guard();
DROP INDEX IF EXISTS project_workflow_scheme_idx;

UPDATE project p SET workflow_scheme_id = (
    SELECT id FROM workflow_scheme s WHERE s.org_id = p.org_id AND s.is_default
) WHERE p.workflow_scheme_id IS NULL;

ALTER TABLE project ALTER COLUMN workflow_scheme_id SET NOT NULL;
DROP INDEX IF EXISTS workflow_scheme_one_default_idx;
ALTER TABLE workflow_scheme DROP COLUMN IF EXISTS is_default;
