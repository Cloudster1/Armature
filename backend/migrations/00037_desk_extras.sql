-- +goose Up
-- The desk's extras: articles a customer reads before raising a request,
-- replies an agent keeps ready, a rating a customer gives once a request is
-- resolved, and the hours a project's goals count in.

CREATE TABLE kb_article (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    published  boolean NOT NULL DEFAULT false,
    author_id  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT kb_article_title_not_blank CHECK (btrim(title) <> '')
);

CREATE UNIQUE INDEX kb_article_project_title_idx ON kb_article (project_id, lower(title));
CREATE INDEX kb_article_search_idx ON kb_article USING gin (to_tsvector('simple', title || ' ' || body));

CREATE TRIGGER kb_article_set_updated_at BEFORE UPDATE ON kb_article
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Articles are a desk's; a software project has no customers to read them.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kb_article_service_project() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM project WHERE id = NEW.project_id AND org_id = NEW.org_id AND kind = 'service') THEN
        RAISE EXCEPTION 'articles belong to a service desk project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER kb_article_service_project
    BEFORE INSERT OR UPDATE ON kb_article
    FOR EACH ROW EXECUTE FUNCTION kb_article_service_project();

CREATE TABLE canned_response (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name       text NOT NULL,
    body       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT canned_response_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT canned_response_body_not_blank CHECK (btrim(body) <> '')
);

CREATE UNIQUE INDEX canned_response_project_name_idx ON canned_response (project_id, lower(name));

CREATE TRIGGER canned_response_set_updated_at BEFORE UPDATE ON canned_response
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- One rating per request, invited by the resolution mail through a token the
-- customer alone holds, kept as a digest.
CREATE TABLE csat_rating (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id   uuid PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    score      integer,
    comment    text NOT NULL DEFAULT '',
    sent_at    timestamptz NOT NULL DEFAULT now(),
    rated_at   timestamptz,
    CONSTRAINT csat_rating_score_range CHECK (score IS NULL OR score BETWEEN 1 AND 5)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION csat_rating_service_issue() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM issue i JOIN project p ON p.id = i.project_id
        WHERE i.id = NEW.issue_id AND i.org_id = NEW.org_id AND p.kind = 'service'
    ) THEN
        RAISE EXCEPTION 'a rating belongs to a request of a service desk'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER csat_rating_service_issue
    BEFORE INSERT OR UPDATE ON csat_rating
    FOR EACH ROW EXECUTE FUNCTION csat_rating_service_issue();

-- When a desk is open: hours per weekday as [["09:00","17:00"]], days off,
-- and the zone the hours are read in. Goals that say so count only these.
CREATE TABLE business_calendar (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id uuid NOT NULL UNIQUE REFERENCES project(id) ON DELETE CASCADE,
    timezone   text NOT NULL DEFAULT 'UTC',
    hours      jsonb NOT NULL DEFAULT '{}'::jsonb,
    holidays   date[] NOT NULL DEFAULT '{}',
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT business_calendar_hours_object CHECK (jsonb_typeof(hours) = 'object')
);

CREATE TRIGGER business_calendar_set_updated_at BEFORE UPDATE ON business_calendar
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE sla_policy ADD COLUMN use_calendar boolean NOT NULL DEFAULT false;

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['kb_article', 'canned_response', 'csat_rating', 'business_calendar']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON kb_article, canned_response, csat_rating, business_calendar TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE sla_policy DROP COLUMN IF EXISTS use_calendar;
DROP TABLE IF EXISTS business_calendar CASCADE;
DROP TABLE IF EXISTS csat_rating CASCADE;
DROP FUNCTION IF EXISTS csat_rating_service_issue();
DROP TABLE IF EXISTS canned_response CASCADE;
DROP TABLE IF EXISTS kb_article CASCADE;
DROP FUNCTION IF EXISTS kb_article_service_project();
