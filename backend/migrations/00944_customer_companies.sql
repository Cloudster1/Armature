-- +goose Up
-- A desk's customers in companies (#78), whose members read the requests a
-- colleague shared with them. The api decides who sees what; the keys and
-- triggers here refuse what no request could be.

CREATE TABLE customer_company (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name       text NOT NULL,
    -- A customer at one of these joins on their first request at the desk.
    domains    text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT customer_company_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 100),
    CONSTRAINT customer_company_domains_shape CHECK (domain_list_well_formed(domains)),
    -- What a member and an issue name together, so neither can be of two desks.
    CONSTRAINT customer_company_id_project_key UNIQUE (id, project_id)
);

CREATE UNIQUE INDEX customer_company_name_idx ON customer_company (project_id, lower(name));

CREATE TRIGGER customer_company_set_updated_at BEFORE UPDATE ON customer_company
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A company is a desk's, for good, and no two of a desk take the same domain,
-- or a customer at it would join both.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION customer_company_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.project_id <> OLD.project_id THEN
        RAISE EXCEPTION 'a company stays with its desk'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM project WHERE id = NEW.project_id AND kind = 'service') THEN
        RAISE EXCEPTION 'a company belongs to a service desk project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF EXISTS (SELECT 1 FROM customer_company c
               WHERE c.project_id = NEW.project_id AND c.id <> NEW.id AND c.domains && NEW.domains) THEN
        RAISE EXCEPTION 'another company of the desk takes one of these domains'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER customer_company_check
    BEFORE INSERT OR UPDATE ON customer_company
    FOR EACH ROW EXECUTE FUNCTION customer_company_guard();

-- The desk is repeated so that one key holds a customer to one company of it.
CREATE TABLE customer_company_member (
    company_id uuid NOT NULL,
    project_id uuid NOT NULL,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    added_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, user_id),
    CONSTRAINT customer_company_member_company_fkey FOREIGN KEY (company_id, project_id)
        REFERENCES customer_company (id, project_id) ON DELETE CASCADE,
    CONSTRAINT customer_company_member_one_per_desk UNIQUE (project_id, user_id)
);

CREATE INDEX customer_company_member_user_idx ON customer_company_member (user_id);

-- Only a customer is in a company: an agent sees every request anyway.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION customer_company_member_guard() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing = 'customer') THEN
        RAISE EXCEPTION 'only a customer is a member of a company'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER customer_company_member_check
    BEFORE INSERT OR UPDATE ON customer_company_member
    FOR EACH ROW EXECUTE FUNCTION customer_company_member_guard();

-- Somebody who stops being a customer leaves their companies, so a member is
-- a customer whichever way the standing changed.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_user_leaves_companies() RETURNS trigger AS $$
BEGIN
    IF OLD.standing = 'customer' AND NEW.standing IS DISTINCT FROM 'customer' THEN
        DELETE FROM customer_company_member WHERE user_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER app_user_leaves_companies AFTER UPDATE OF standing ON app_user
    FOR EACH ROW EXECUTE FUNCTION app_user_leaves_companies();

-- The reporter's company when the request was raised, of the request's own
-- desk, and whether the reporter let their colleagues read it.
ALTER TABLE issue
    ADD COLUMN company_id uuid,
    ADD COLUMN shared_with_company boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT issue_company_fkey FOREIGN KEY (company_id, project_id)
        REFERENCES customer_company (id, project_id) ON DELETE SET NULL (company_id);

CREATE INDEX issue_company_idx ON issue (company_id) WHERE company_id IS NOT NULL;

-- Shared is shared with a company; when the company goes, so does the sharing.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_company_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.company_id IS NOT NULL AND NEW.company_id IS NULL THEN
        NEW.shared_with_company := false;
    END IF;
    IF NEW.shared_with_company AND NEW.company_id IS NULL THEN
        RAISE EXCEPTION 'a request is shared with the company it came from, and this one came from none'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_company_check
    BEFORE INSERT OR UPDATE OF company_id, shared_with_company ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_company_guard();

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON customer_company, customer_company_member TO armature_app, armature_admin;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS issue_company_check ON issue;
DROP FUNCTION IF EXISTS issue_company_guard();
DROP INDEX IF EXISTS issue_company_idx;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_company_fkey,
    DROP COLUMN IF EXISTS shared_with_company,
    DROP COLUMN IF EXISTS company_id;
DROP TRIGGER IF EXISTS app_user_leaves_companies ON app_user;
DROP FUNCTION IF EXISTS app_user_leaves_companies();
DROP TABLE IF EXISTS customer_company_member, customer_company;
DROP FUNCTION IF EXISTS customer_company_member_guard();
DROP FUNCTION IF EXISTS customer_company_guard();
