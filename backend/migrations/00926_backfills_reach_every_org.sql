-- +goose Up
-- Three backfills read org with its row security forced on the owner, so on a
-- database migrated as a plain owner they reached no organization: the
-- Initiative type (00005), the automation account (00033) and the default
-- holiday calendar (00909). This runs them again with row security lifted on
-- org and on the tables they fill, as 00904 does for the roles, and adds
-- nothing where an organization already has what it would add.
ALTER TABLE org NO FORCE ROW LEVEL SECURITY;
ALTER TABLE org_member NO FORCE ROW LEVEL SECURITY;
ALTER TABLE issue_type NO FORCE ROW LEVEL SECURITY;
ALTER TABLE holiday_calendar NO FORCE ROW LEVEL SECURITY;

INSERT INTO issue_type (org_id, name, description, icon, hierarchy_level, position)
SELECT o.id, 'Initiative', 'A goal several epics add up to.', 'initiative', 2, 5
FROM org o
WHERE NOT EXISTS (
    SELECT 1 FROM issue_type t WHERE t.org_id = o.id AND lower(t.name) = 'initiative'
);

-- An account left behind under the address is taken up again rather than
-- refused by the unique address.
-- +goose StatementBegin
DO $$
DECLARE
    o record;
    uid uuid;
    address text;
BEGIN
    FOR o IN SELECT id FROM org WHERE automation_user_id IS NULL LOOP
        address := 'automation+' || o.id || '@armature.invalid';
        SELECT id INTO uid FROM app_user WHERE email = address;
        IF uid IS NULL THEN
            INSERT INTO app_user (email, name, is_active)
            VALUES (address, 'Automation', false)
            RETURNING id INTO uid;
        END IF;
        INSERT INTO org_member (org_id, user_id, org_role) VALUES (o.id, uid, 'member')
            ON CONFLICT DO NOTHING;
        UPDATE org SET automation_user_id = uid WHERE id = o.id;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- A calendar of the default's name that lost its standing keeps it; the
-- organization goes on without a default rather than failing the upgrade.
INSERT INTO holiday_calendar (org_id, name, is_default)
SELECT o.id, 'Holidays', true
FROM org o
WHERE NOT EXISTS (
    SELECT 1 FROM holiday_calendar c WHERE c.org_id = o.id AND c.is_default
)
ON CONFLICT DO NOTHING;

ALTER TABLE holiday_calendar FORCE ROW LEVEL SECURITY;
ALTER TABLE issue_type FORCE ROW LEVEL SECURITY;
ALTER TABLE org_member FORCE ROW LEVEL SECURITY;
ALTER TABLE org FORCE ROW LEVEL SECURITY;

-- +goose Down
-- Nothing to take back: these are what every organization is made with.
SELECT 1;
