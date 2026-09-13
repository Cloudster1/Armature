-- +goose Up
-- Every mail the desk consumed, and what became of it. The message id is
-- unique, so a mail delivered twice is refused by the row, not by memory.
CREATE TABLE inbound_mail (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    -- Null when the mail could not be placed in any organization.
    org_id      uuid REFERENCES org(id) ON DELETE CASCADE,
    message_id  text NOT NULL,
    from_email  citext NOT NULL,
    subject     text NOT NULL DEFAULT '',
    issue_id    uuid REFERENCES issue(id) ON DELETE SET NULL,
    comment_id  uuid REFERENCES issue_comment(id) ON DELETE SET NULL,
    outcome     text NOT NULL CHECK (outcome IN ('processing', 'commented', 'refused', 'unmatched', 'ambiguous', 'empty')),
    detail      text NOT NULL DEFAULT '',
    received_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inbound_mail_message_id_key UNIQUE (message_id)
);

-- The worker writes on the admin path, since a mail names no tenant until it
-- is read; the policy keeps one organization's mail from another's eyes.
-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE inbound_mail ENABLE ROW LEVEL SECURITY;
    ALTER TABLE inbound_mail FORCE ROW LEVEL SECURITY;
    CREATE POLICY inbound_mail_tenant_isolation ON inbound_mail
        USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY inbound_mail_admin_bypass ON inbound_mail TO armature_admin USING (true) WITH CHECK (true);
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON inbound_mail TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS inbound_mail;
