-- +goose Up
-- A connected app sets up its own webhook once an administrator allowed it
-- (#281). The api decides what an app may change; the keys and triggers here
-- refuse a second endpoint for one app and an endpoint for an app nobody allowed.

-- A partner's own endpoint, at most one per client, gone with the client.
ALTER TABLE webhook_endpoint
    ADD COLUMN client_id uuid UNIQUE REFERENCES oauth_client(id) ON DELETE CASCADE;

-- An endpoint is named after its client, following a rename.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION oauth_client_renames_endpoint() RETURNS trigger AS $$
BEGIN
    IF NEW.name IS DISTINCT FROM OLD.name THEN
        UPDATE webhook_endpoint SET name = NEW.name || ' (connected app)' WHERE client_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER oauth_client_renames_endpoint AFTER UPDATE OF name ON oauth_client
    FOR EACH ROW EXECUTE FUNCTION oauth_client_renames_endpoint();

-- An endpoint is a client's only once an administrator allowed the client.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION webhook_endpoint_client_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.client_id IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM oauth_client WHERE id = NEW.client_id AND status = 'active') THEN
        RAISE EXCEPTION 'a connected app is allowed by an administrator before it has a webhook'
            USING ERRCODE = 'check_violation';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.client_id IS DISTINCT FROM OLD.client_id THEN
        RAISE EXCEPTION 'a webhook stays with the connected app it was made for'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER webhook_endpoint_client_check
    BEFORE INSERT OR UPDATE OF client_id ON webhook_endpoint
    FOR EACH ROW EXECUTE FUNCTION webhook_endpoint_client_guard();

-- +goose Down
DROP TRIGGER IF EXISTS webhook_endpoint_client_check ON webhook_endpoint;
DROP FUNCTION IF EXISTS webhook_endpoint_client_guard();
DROP TRIGGER IF EXISTS oauth_client_renames_endpoint ON oauth_client;
DROP FUNCTION IF EXISTS oauth_client_renames_endpoint();
DELETE FROM webhook_endpoint WHERE client_id IS NOT NULL;
ALTER TABLE webhook_endpoint DROP COLUMN IF EXISTS client_id;
