-- +goose Up
-- An installation is one team's tracker, with one organization in it. A
-- database holding several is refused rather than merged: their project keys,
-- statuses, roles and people would collide. The operator deletes the extra
-- ones with the release before this one, which still has the way to, and
-- upgrades again. Nothing here changes the schema.
--
-- The owner a cluster migrates as sees no row of org while its row security is
-- forced, so it is lifted to count them; the refusal rolls the lift back too.
ALTER TABLE org NO FORCE ROW LEVEL SECURITY;

-- +goose StatementBegin
DO $$
DECLARE
    listed CONSTANT int := 10;
    total int;
    names text;
BEGIN
    SELECT count(*) INTO total FROM org;
    IF total <= 1 THEN
        RETURN;
    END IF;
    SELECT string_agg(format('%s (%s)', slug, name), ', ' ORDER BY created_at, slug) INTO names
    FROM (SELECT slug, name, created_at FROM org ORDER BY created_at, slug LIMIT listed) first_ones;
    IF total > listed THEN
        names := names || format(' and %s more', total - listed);
    END IF;
    RAISE EXCEPTION 'This installation holds % organizations, and Armature now runs one per installation: %. Delete the extra ones with the previous release before upgrading.', total, names
        USING ERRCODE = 'check_violation';
END;
$$;
-- +goose StatementEnd

ALTER TABLE org FORCE ROW LEVEL SECURITY;

-- +goose Down
-- The guard changed nothing, so there is nothing to take back.
