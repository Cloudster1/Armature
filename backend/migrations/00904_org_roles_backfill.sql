-- +goose Up
-- The first run of the roles migration filled no organization on a database
-- whose migrations run as a plain owner: the org table forces row security
-- on the owner, so the loop over organizations saw none. This does the same
-- backfill with row security lifted for the loop, on the table it reads and
-- on the one it fills; it adds nothing where the roles are already there.
ALTER TABLE org NO FORCE ROW LEVEL SECURITY;
ALTER TABLE org_role NO FORCE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$
DECLARE
    o uuid;
BEGIN
    FOR o IN SELECT id FROM org LOOP
        PERFORM org_builtin_roles(o);
    END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE org_role FORCE ROW LEVEL SECURITY;
ALTER TABLE org FORCE ROW LEVEL SECURITY;

-- +goose Down
-- Nothing to take back: the roles are what every organization has.
SELECT 1;
