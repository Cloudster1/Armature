-- +goose Up
-- Git and CI: repositories connected to a project, and what flows in from them.

CREATE TYPE git_host AS ENUM ('github', 'gitlab');

-- A repository is connected to one project. The webhook secret authenticates
-- what the host sends us; the access token, when there is one, is what lets us
-- reach back to create branches and comment on pull requests. Both are stored
-- as given because both have to be used, not merely compared.
CREATE TABLE git_repository (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id              uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id          uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    host                git_host NOT NULL,
    -- "owner/name" on the host.
    name                text NOT NULL,
    url                 text NOT NULL,
    -- Where the host's API answers. The public hosts have one address; a
    -- self-hosted GitLab or GitHub Enterprise has its own.
    api_base_url        text NOT NULL,
    default_branch      text NOT NULL DEFAULT 'main',
    webhook_secret      text NOT NULL,
    access_token        text NOT NULL DEFAULT '',
    -- The transition to take on every issue a pull request names when that
    -- pull request is merged. Empty means merging changes nothing here.
    transition_on_merge text NOT NULL DEFAULT '',
    -- Whoever connected the repository stands in as the actor for a commit
    -- whose author is not a member here.
    connected_by        uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT git_repository_name_shape CHECK (name ~ '^[^/[:space:]]+(/[^/[:space:]]+)+$')
);

CREATE UNIQUE INDEX git_repository_project_name_idx ON git_repository (project_id, host, lower(name));
CREATE INDEX git_repository_project_idx ON git_repository (project_id);

CREATE TRIGGER git_repository_set_updated_at BEFORE UPDATE ON git_repository
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE git_commit (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES git_repository(id) ON DELETE CASCADE,
    sha           text NOT NULL,
    message       text NOT NULL,
    author_name   text NOT NULL DEFAULT '',
    author_email  text NOT NULL DEFAULT '',
    url           text NOT NULL DEFAULT '',
    branch        text NOT NULL DEFAULT '',
    committed_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT git_commit_sha_shape CHECK (sha ~ '^[0-9a-f]{7,64}$')
);

CREATE UNIQUE INDEX git_commit_repository_sha_idx ON git_commit (repository_id, sha);

-- A commit names the issues it is about by key in its message. The link is a
-- row rather than a re-parse, so a renamed project cannot lose the history.
CREATE TABLE issue_commit (
    org_id    uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id  uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    commit_id uuid NOT NULL REFERENCES git_commit(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, commit_id)
);

CREATE INDEX issue_commit_commit_idx ON issue_commit (commit_id);

CREATE TABLE git_branch (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES git_repository(id) ON DELETE CASCADE,
    name          text NOT NULL,
    url           text NOT NULL DEFAULT '',
    -- The issue the branch is for, found by the key in its name.
    issue_id      uuid REFERENCES issue(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT git_branch_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX git_branch_repository_name_idx ON git_branch (repository_id, name);
CREATE INDEX git_branch_issue_idx ON git_branch (issue_id) WHERE issue_id IS NOT NULL;

CREATE TYPE pull_request_state AS ENUM ('open', 'merged', 'closed');

CREATE TABLE pull_request (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES git_repository(id) ON DELETE CASCADE,
    number        bigint NOT NULL,
    title         text NOT NULL,
    url           text NOT NULL DEFAULT '',
    state         pull_request_state NOT NULL DEFAULT 'open',
    source_branch text NOT NULL DEFAULT '',
    target_branch text NOT NULL DEFAULT '',
    author_name   text NOT NULL DEFAULT '',
    head_sha      text NOT NULL DEFAULT '',
    opened_at     timestamptz NOT NULL DEFAULT now(),
    merged_at     timestamptz,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX pull_request_repository_number_idx ON pull_request (repository_id, number);

CREATE TRIGGER pull_request_set_updated_at BEFORE UPDATE ON pull_request
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE issue_pull_request (
    org_id          uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id        uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    pull_request_id uuid NOT NULL REFERENCES pull_request(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, pull_request_id)
);

CREATE INDEX issue_pull_request_pr_idx ON issue_pull_request (pull_request_id);

CREATE TYPE ci_status AS ENUM ('pending', 'success', 'failure', 'cancelled');

-- One run of a check, workflow or pipeline against a commit. What the host
-- calls it varies; what matters here is which commit, which branch, and how
-- it came out.
CREATE TABLE ci_run (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES git_repository(id) ON DELETE CASCADE,
    external_id   text NOT NULL,
    name          text NOT NULL,
    status        ci_status NOT NULL,
    url           text NOT NULL DEFAULT '',
    sha           text NOT NULL,
    branch        text NOT NULL DEFAULT '',
    started_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ci_run_repository_external_idx ON ci_run (repository_id, external_id);
CREATE INDEX ci_run_sha_idx ON ci_run (repository_id, sha);

CREATE TRIGGER ci_run_set_updated_at BEFORE UPDATE ON ci_run
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'git_repository', 'git_commit', 'issue_commit', 'git_branch',
        'pull_request', 'issue_pull_request', 'ci_run'
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON git_repository, git_commit, issue_commit,
            git_branch, pull_request, issue_pull_request, ci_run TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS ci_run, issue_pull_request, pull_request, git_branch,
    issue_commit, git_commit, git_repository CASCADE;
DROP TYPE IF EXISTS ci_status, pull_request_state, git_host;
