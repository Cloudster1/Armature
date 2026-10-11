-- +goose Up
-- An attachment row leaves a tombstone however it goes, so a cascade down an
-- issue's subtree or out of a deleted organization cannot strand its bytes.
-- A deleted organization is gone before its cascade gets here, so its
-- tombstones name none; the reaper reads across tenants and finds them anyway.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION attachment_leaves_tombstone() RETURNS trigger AS $$
BEGIN
    INSERT INTO attachment_tombstone (object_key, org_id)
    VALUES (OLD.object_key, (SELECT id FROM org WHERE id = OLD.org_id))
    ON CONFLICT (object_key) DO NOTHING;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER attachment_tombstone_on_delete
    AFTER DELETE ON attachment
    FOR EACH ROW EXECUTE FUNCTION attachment_leaves_tombstone();

-- +goose Down
DROP TRIGGER IF EXISTS attachment_tombstone_on_delete ON attachment;
DROP FUNCTION IF EXISTS attachment_leaves_tombstone();
