-- +goose Up
-- Nobody learns an issue through watching it unless they may read it (#409).
-- The rule mirrors issue.mayWatch: a request takes anyone, since the desk
-- tells its followers only the replies; any other issue takes whoever holds
-- the read permission over its project, or is the owner.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_watcher_reads() RETURNS trigger AS $$
DECLARE
    in_project uuid;
    is_request boolean;
BEGIN
    SELECT i.project_id, p.kind = 'service' AND i.request_type_id IS NOT NULL
      INTO in_project, is_request
      FROM issue i JOIN project p ON p.id = i.project_id
     WHERE i.id = NEW.issue_id;
    IF is_request THEN
        RETURN NEW;
    END IF;
    IF EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing = 'owner') THEN
        RETURN NEW;
    END IF;
    IF EXISTS (
        SELECT 1
          FROM role_assignment a
          JOIN role r ON r.key = a.role AND 'read' = ANY (r.permissions)
          LEFT JOIN group_member m ON m.group_id = a.group_id AND m.user_id = NEW.user_id
         WHERE (a.user_id = NEW.user_id OR m.user_id IS NOT NULL)
           AND (a.project_id IS NULL OR a.project_id = in_project)
    ) THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'a watcher must be able to read the issue''s project'
        USING ERRCODE = 'check_violation';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_watcher_reads
    BEFORE INSERT OR UPDATE OF user_id, issue_id ON issue_watcher
    FOR EACH ROW EXECUTE FUNCTION issue_watcher_reads();

-- A row somebody wants by mail alone is kept, so a second pass of the same
-- event finds it and sends nothing again, but the inbox does not show it (#410).
ALTER TABLE notification ADD COLUMN hidden boolean NOT NULL DEFAULT false;

-- Nothing ever wrote a rule or a saved search notification; the subscription
-- mail is its own opt-in (#411). The kinds go, and so do switches for them.
DELETE FROM notification WHERE kind IN ('rule', 'filter');
ALTER TABLE notification DROP CONSTRAINT notification_kind_known;
ALTER TABLE notification ADD CONSTRAINT notification_kind_known CHECK (kind IN
    ('assigned', 'mentioned', 'commented', 'transitioned', 'watching', 'sla_breached', 'absence'));
UPDATE notification_preference
   SET mail = mail - 'rule' - 'filter', inapp = inapp - 'rule' - 'filter'
 WHERE mail ?| ARRAY['rule', 'filter'] OR inapp ?| ARRAY['rule', 'filter'];

-- +goose Down
ALTER TABLE notification DROP CONSTRAINT notification_kind_known;
ALTER TABLE notification ADD CONSTRAINT notification_kind_known CHECK (kind IN
    ('assigned', 'mentioned', 'commented', 'transitioned', 'watching', 'sla_breached', 'rule', 'filter', 'absence'));
DELETE FROM notification WHERE hidden;
ALTER TABLE notification DROP COLUMN hidden;
DROP TRIGGER IF EXISTS issue_watcher_reads ON issue_watcher;
DROP FUNCTION IF EXISTS issue_watcher_reads();
