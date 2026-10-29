-- +goose Up
-- A person who may not see an issue can ask for access to it (#279). The row
-- exists only when the issue does and the asker may not read its project, so
-- nothing about it ever reaches the asker; the project's administrators are
-- told, and one of them grants the reader role from it.
CREATE TABLE access_request (
    id           uuid        PRIMARY KEY DEFAULT uuidv7(),
    project_id   uuid        NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    requester_id uuid        NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    issue_key    text        NOT NULL,
    message      text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    granted_at   timestamptz,
    granted_by   uuid        REFERENCES app_user (id) ON DELETE SET NULL,
    CONSTRAINT access_request_message_short CHECK (char_length(message) <= 1000),
    -- Who granted it may later leave; when it was granted stays.
    CONSTRAINT access_request_grant_has_time CHECK (granted_by IS NULL OR granted_at IS NOT NULL)
);
-- Repeated asks by one person for one project within a day fold into the first.
CREATE INDEX access_request_recent_idx ON access_request (project_id, requester_id, created_at DESC);

ALTER TABLE notification DROP CONSTRAINT notification_kind_known;
ALTER TABLE notification ADD CONSTRAINT notification_kind_known CHECK (kind IN
    ('assigned', 'mentioned', 'commented', 'transitioned', 'watching', 'sla_breached', 'absence', 'access_requested'));

-- A project may say that its issues exist to people who may not see them, so
-- a partner's page can show "restricted" instead of "not found". Off unless
-- its administrators turn it on, since that is what it reveals.
ALTER TABLE project ADD COLUMN reveals_issues boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE project DROP COLUMN reveals_issues;
DELETE FROM notification WHERE kind = 'access_requested';
ALTER TABLE notification DROP CONSTRAINT notification_kind_known;
ALTER TABLE notification ADD CONSTRAINT notification_kind_known CHECK (kind IN
    ('assigned', 'mentioned', 'commented', 'transitioned', 'watching', 'sla_breached', 'absence'));
DROP TABLE access_request;
