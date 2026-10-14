-- +goose Up
-- The api stops naming the organization (#218): an installation holds one, so a
-- row written without org_id is the installation's. The default is found when a
-- row is written, not now, because a fresh database has no organization yet
-- when it migrates. Step 5 drops the columns and this function with them.
-- +goose StatementBegin
CREATE FUNCTION installation_org_id() RETURNS uuid
LANGUAGE plpgsql STABLE PARALLEL SAFE AS $$
DECLARE
    total int;
    found uuid;
BEGIN
    SELECT count(*), (array_agg(o.id))[1] INTO total, found FROM org o;
    IF total > 1 THEN
        RAISE EXCEPTION 'This installation holds % organizations, and Armature runs one per installation, so a row cannot tell which it belongs to. Delete the extra ones, then try again.', total
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN found;
END $$;
-- +goose StatementEnd

-- What the inserts and filters that still name it read, whether or not a
-- session sets app.org_id; nothing does any more. A filter calls it for every
-- row it looks at, so it reads the row plainly: the default, called once per
-- row written, is the one that refuses a second organization.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT id FROM org
$$;
-- +goose StatementEnd

-- Every row that must name an organization. The two that may name none, a mail
-- that matched no request and a file's tombstone, keep naming none.
-- +goose StatementBegin
DO $$
DECLARE
    t record;
BEGIN
    FOR t IN
        SELECT c.oid::regclass AS name
        FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
        WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
          AND a.attname = 'org_id' AND NOT a.attisdropped AND a.attnotnull
        ORDER BY c.relname
    LOOP
        EXECUTE format('ALTER TABLE %s ALTER COLUMN org_id SET DEFAULT installation_org_id()', t.name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE
    t record;
BEGIN
    FOR t IN
        SELECT c.oid::regclass AS name
        FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
        JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
        WHERE c.relnamespace = 'public'::regnamespace AND a.attname = 'org_id'
          AND pg_get_expr(d.adbin, d.adrelid) = 'installation_org_id()'
        ORDER BY c.relname
    LOOP
        EXECUTE format('ALTER TABLE %s ALTER COLUMN org_id DROP DEFAULT', t.name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT NULLIF(current_setting('app.org_id', true), '')::uuid
$$;
-- +goose StatementEnd

DROP FUNCTION installation_org_id();
