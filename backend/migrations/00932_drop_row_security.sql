-- +goose Up
-- Row security kept organizations apart, and an installation holds one, so it
-- keeps nothing apart any more (#218). The walls inside an installation are
-- projects, customers and owners, and the api and the triggers keep those.
-- Only the policies the migrations made are dropped: one somebody added by
-- hand stops the upgrade instead of going with them unseen.
-- +goose StatementBegin
DO $$
DECLARE
    p record;
    t record;
    left_over text;
BEGIN
    FOR p IN
        SELECT tablename, policyname FROM pg_policies
        WHERE schemaname = 'public'
          AND policyname IN (tablename || '_tenant_isolation', tablename || '_admin_bypass')
    LOOP
        EXECUTE format('DROP POLICY %I ON %I', p.policyname, p.tablename);
    END LOOP;

    SELECT string_agg(format('%s on %s', policyname, tablename), ', ' ORDER BY tablename, policyname)
    INTO left_over
    FROM pg_policies WHERE schemaname = 'public';
    IF left_over IS NOT NULL THEN
        RAISE EXCEPTION 'This database has row security policies Armature did not make: %. Drop them, or keep them in a schema of their own, before upgrading.', left_over
            USING ERRCODE = 'object_in_use';
    END IF;

    FOR t IN
        SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
          AND (c.relrowsecurity OR c.relforcerowsecurity)
    LOOP
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY, DISABLE ROW LEVEL SECURITY', t.relname);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- The policies come back on every table that still carries the organization,
-- as each migration made them: the tenant's, and the admin role's bypass
-- where that role exists.
-- +goose StatementBegin
DO $$
DECLARE
    t record;
    bypass boolean := EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin');
BEGIN
    FOR t IN
        SELECT c.relname, a.attname
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_attribute a ON a.attrelid = c.oid AND NOT a.attisdropped
         AND a.attname = CASE WHEN c.relname = 'org' THEN 'id' ELSE 'org_id' END
        WHERE n.nspname = 'public' AND c.relkind = 'r'
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY, FORCE ROW LEVEL SECURITY', t.relname);
        EXECUTE format('CREATE POLICY %I ON %I USING (%I = current_org_id()) WITH CHECK (%I = current_org_id())',
            t.relname || '_tenant_isolation', t.relname, t.attname, t.attname);
        IF bypass THEN
            EXECUTE format('CREATE POLICY %I ON %I TO armature_admin USING (true) WITH CHECK (true)',
                t.relname || '_admin_bypass', t.relname);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd
