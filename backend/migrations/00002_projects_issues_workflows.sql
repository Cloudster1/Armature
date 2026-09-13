-- +goose Up
-- Projects, issues and the workflow engine they move through.

-- ------------------------------------------------------------ statuses ----

-- A status belongs to exactly one of three categories. The category is what
-- boards, reports and "is it done yet" questions actually key on; the status
-- name is what people argue about.
CREATE TYPE status_category AS ENUM ('todo', 'in_progress', 'done');

CREATE TABLE issue_status (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name        text NOT NULL,
    category    status_category NOT NULL,
    description text NOT NULL DEFAULT '',
    position    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_status_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX issue_status_org_name_idx ON issue_status (org_id, lower(name));

CREATE TRIGGER issue_status_set_updated_at BEFORE UPDATE ON issue_status
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------- issue types ----

CREATE TABLE issue_type (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    icon        text NOT NULL DEFAULT 'task',
    -- A subtask type may only ever exist under a parent issue.
    is_subtask  boolean NOT NULL DEFAULT false,
    position    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_type_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX issue_type_org_name_idx ON issue_type (org_id, lower(name));

CREATE TRIGGER issue_type_set_updated_at BEFORE UPDATE ON issue_type
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ------------------------------------------------------------ workflows ----

CREATE TABLE workflow (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workflow_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX workflow_org_name_idx ON workflow (org_id, lower(name));

CREATE TRIGGER workflow_set_updated_at BEFORE UPDATE ON workflow
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Which statuses a workflow uses. The same status can appear in many workflows,
-- which is what lets two projects share the meaning of "In Progress" while
-- disagreeing about how an issue gets there.
CREATE TABLE workflow_step (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    workflow_id uuid NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    status_id   uuid NOT NULL REFERENCES issue_status(id) ON DELETE RESTRICT,
    -- Exactly one step per workflow is where newly created issues land.
    is_initial  boolean NOT NULL DEFAULT false,
    position    integer NOT NULL DEFAULT 0,
    UNIQUE (workflow_id, status_id)
);

CREATE INDEX workflow_step_workflow_idx ON workflow_step (workflow_id);

-- One initial step per workflow, enforced by the database rather than by hope.
CREATE UNIQUE INDEX workflow_step_single_initial_idx ON workflow_step (workflow_id)
    WHERE is_initial;

CREATE TABLE workflow_transition (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    workflow_id  uuid NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    name         text NOT NULL,
    -- A null source means the transition is available from every status, which
    -- is how "Close" or "Reopen from anywhere" is expressed.
    from_step_id uuid REFERENCES workflow_step(id) ON DELETE CASCADE,
    to_step_id   uuid NOT NULL REFERENCES workflow_step(id) ON DELETE CASCADE,
    description  text NOT NULL DEFAULT '',
    position     integer NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workflow_transition_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT workflow_transition_not_a_self_loop CHECK (from_step_id IS DISTINCT FROM to_step_id)
);

CREATE INDEX workflow_transition_workflow_idx ON workflow_transition (workflow_id);
CREATE INDEX workflow_transition_from_idx ON workflow_transition (from_step_id);

-- Conditions decide whether a transition is offered, validators whether the
-- input is acceptable, and post-functions what happens once it is taken. Each
-- is a named implementation plus its configuration, so a new rule type needs no
-- schema change.
CREATE TYPE transition_rule_kind AS ENUM ('condition', 'validator', 'postfunction');

CREATE TABLE transition_rule (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    transition_id uuid NOT NULL REFERENCES workflow_transition(id) ON DELETE CASCADE,
    kind          transition_rule_kind NOT NULL,
    rule_type     text NOT NULL,
    config        jsonb NOT NULL DEFAULT '{}'::jsonb,
    position      integer NOT NULL DEFAULT 0,
    CONSTRAINT transition_rule_type_not_blank CHECK (btrim(rule_type) <> '')
);

CREATE INDEX transition_rule_transition_idx ON transition_rule (transition_id, kind, position);

-- A scheme maps issue types to workflows for a project. The row with a null
-- issue type is the fallback for any type not named explicitly.
CREATE TABLE workflow_scheme (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workflow_scheme_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX workflow_scheme_org_name_idx ON workflow_scheme (org_id, lower(name));

CREATE TRIGGER workflow_scheme_set_updated_at BEFORE UPDATE ON workflow_scheme
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE workflow_scheme_item (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    scheme_id     uuid NOT NULL REFERENCES workflow_scheme(id) ON DELETE CASCADE,
    issue_type_id uuid REFERENCES issue_type(id) ON DELETE CASCADE,
    workflow_id   uuid NOT NULL REFERENCES workflow(id) ON DELETE RESTRICT
);

-- One mapping per issue type, and one fallback, per scheme.
CREATE UNIQUE INDEX workflow_scheme_item_type_idx ON workflow_scheme_item (scheme_id, issue_type_id)
    WHERE issue_type_id IS NOT NULL;
CREATE UNIQUE INDEX workflow_scheme_item_default_idx ON workflow_scheme_item (scheme_id)
    WHERE issue_type_id IS NULL;

-- ------------------------------------------------------------- projects ----

CREATE TYPE project_kind AS ENUM ('software', 'service', 'business');

CREATE TABLE project (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id            uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    key               text NOT NULL,
    name              text NOT NULL,
    description       text NOT NULL DEFAULT '',
    kind              project_kind NOT NULL DEFAULT 'software',
    lead_id           uuid REFERENCES app_user(id) ON DELETE SET NULL,
    workflow_scheme_id uuid NOT NULL REFERENCES workflow_scheme(id) ON DELETE RESTRICT,
    -- The counter behind human readable keys such as NOJ-142. Incremented with
    -- UPDATE ... RETURNING inside the same transaction that creates the issue,
    -- so two concurrent creators can never be handed the same number.
    issue_seq         bigint NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    archived_at       timestamptz,
    CONSTRAINT project_name_not_blank CHECK (btrim(name) <> ''),
    -- Two to ten upper case letters or digits, starting with a letter: short
    -- enough to prefix every issue key without becoming noise.
    CONSTRAINT project_key_shape CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$')
);

CREATE UNIQUE INDEX project_org_key_idx ON project (org_id, key);
CREATE INDEX project_org_active_idx ON project (org_id) WHERE archived_at IS NULL;

CREATE TRIGGER project_set_updated_at BEFORE UPDATE ON project
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- --------------------------------------------------------------- issues ----

CREATE TYPE issue_priority AS ENUM ('lowest', 'low', 'medium', 'high', 'highest');

CREATE TABLE issue (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id    uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    -- The numeric half of the key. The full key is the project key and this,
    -- joined by a hyphen; it is derived rather than stored so that renaming a
    -- project cannot leave issue keys disagreeing with it.
    key_num       bigint NOT NULL,
    issue_type_id uuid NOT NULL REFERENCES issue_type(id) ON DELETE RESTRICT,
    status_id     uuid NOT NULL REFERENCES issue_status(id) ON DELETE RESTRICT,
    summary       text NOT NULL,
    -- Rich text as ProseMirror JSON rather than markdown, so mentions, panels
    -- and embeds are first class rather than parsed out of prose.
    description   jsonb,
    priority      issue_priority NOT NULL DEFAULT 'medium',
    assignee_id   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    reporter_id   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    parent_id     uuid REFERENCES issue(id) ON DELETE CASCADE,
    due_date      date,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    -- Set by a post-function when the issue reaches a done status, cleared when
    -- it leaves one, so "when was this finished" is answerable without walking
    -- the changelog.
    resolved_at   timestamptz,
    CONSTRAINT issue_summary_not_blank CHECK (btrim(summary) <> ''),
    CONSTRAINT issue_summary_length CHECK (length(summary) <= 255),
    CONSTRAINT issue_not_its_own_parent CHECK (id <> parent_id)
);

CREATE UNIQUE INDEX issue_project_key_idx ON issue (project_id, key_num);
CREATE INDEX issue_org_project_idx ON issue (org_id, project_id);
CREATE INDEX issue_assignee_idx ON issue (org_id, assignee_id) WHERE assignee_id IS NOT NULL;
CREATE INDEX issue_status_idx ON issue (org_id, status_id);
CREATE INDEX issue_parent_idx ON issue (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX issue_updated_idx ON issue (org_id, updated_at DESC);

-- Full text search over the summary. The description is deliberately excluded
-- for now: extracting text from the rich text document belongs with the search
-- work, not here.
CREATE INDEX issue_summary_fts_idx ON issue USING gin (to_tsvector('simple', summary));

CREATE TRIGGER issue_set_updated_at BEFORE UPDATE ON issue
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE issue_comment (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id   uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    author_id  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    body       jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    edited_at  timestamptz
);

CREATE INDEX issue_comment_issue_idx ON issue_comment (issue_id, created_at);

CREATE TRIGGER issue_comment_set_updated_at BEFORE UPDATE ON issue_comment
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The changelog is append only. Every field change and every transition lands
-- here, which is what makes "who moved this to Done, and when" answerable.
CREATE TABLE issue_history (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id   uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    actor_id   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    -- [{field, from, fromLabel, to, toLabel}, ...]
    changes    jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX issue_history_issue_idx ON issue_history (issue_id, created_at DESC);

CREATE TABLE issue_link_type (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name       text NOT NULL,
    outward    text NOT NULL, -- "blocks"
    inward     text NOT NULL, -- "is blocked by"
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_link_type_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX issue_link_type_org_name_idx ON issue_link_type (org_id, lower(name));

CREATE TABLE issue_link (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    link_type_id uuid NOT NULL REFERENCES issue_link_type(id) ON DELETE CASCADE,
    source_id    uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    target_id    uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issue_link_not_self CHECK (source_id <> target_id)
);

CREATE UNIQUE INDEX issue_link_unique_idx ON issue_link (link_type_id, source_id, target_id);
CREATE INDEX issue_link_source_idx ON issue_link (source_id);
CREATE INDEX issue_link_target_idx ON issue_link (target_id);

CREATE TABLE issue_watcher (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id   uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, user_id)
);

CREATE INDEX issue_watcher_user_idx ON issue_watcher (org_id, user_id);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'issue_status', 'issue_type', 'workflow', 'workflow_step',
        'workflow_transition', 'transition_rule', 'workflow_scheme',
        'workflow_scheme_item', 'project', 'issue', 'issue_comment',
        'issue_history', 'issue_link_type', 'issue_link', 'issue_watcher'
    ]
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO armature_app, armature_admin;
        GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS issue_watcher, issue_link, issue_link_type, issue_history,
    issue_comment, issue, project, workflow_scheme_item, workflow_scheme,
    transition_rule, workflow_transition, workflow_step, workflow,
    issue_type, issue_status CASCADE;
DROP TYPE IF EXISTS issue_priority, project_kind, transition_rule_kind, status_category;
