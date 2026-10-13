-- +goose Up
-- A second factor for signing in with a password: an authenticator app's key,
-- sealed by the server with ARMATURE_SECRET_KEY so a copy of this table opens
-- nothing, and a step counter so a code that was used cannot be used again.
-- The row exists from the start of enrolment; confirmed_at says it is in use.
CREATE TABLE user_second_factor (
    user_id       uuid PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    secret_sealed bytea NOT NULL,
    confirmed_at  timestamptz,
    last_step     bigint NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- A code only ever moves forward: a step at or before the last one used is a
-- code somebody already typed, and the database refuses it as the service does.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION second_factor_steps_forward() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.last_step <= OLD.last_step THEN
        RAISE EXCEPTION 'an authenticator code is used once' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER second_factor_steps_forward BEFORE UPDATE OF last_step ON user_second_factor
    FOR EACH ROW EXECUTE FUNCTION second_factor_steps_forward();

-- The codes for when the phone is gone, kept as digests like session tokens.
CREATE TABLE recovery_code (
    user_id   uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    code_hash bytea NOT NULL,
    used_at   timestamptz,
    PRIMARY KEY (user_id, code_hash)
);

-- A used recovery code stays used.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION recovery_code_used_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.used_at IS NOT NULL THEN
        RAISE EXCEPTION 'a recovery code is used once' USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER recovery_code_used_once BEFORE UPDATE ON recovery_code
    FOR EACH ROW EXECUTE FUNCTION recovery_code_used_once();

-- The half of a password sign-in that waits for the code. One per person,
-- replaced by the next sign-in, and only a few wrong codes before it is gone.
CREATE TABLE login_challenge (
    user_id    uuid PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    attempts   int NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_challenge_expires ON login_challenge (expires_at);

-- Who must have a second factor here: nobody, the administrators, or every agent.
ALTER TABLE org ADD COLUMN second_factor text NOT NULL DEFAULT 'off'
    CHECK (second_factor IN ('off', 'admins', 'everyone'));

-- An account belongs to no one organization, so like password_reset there is
-- no tenant to isolate by. The three are read and written on the admin path
-- only, so the application role is given nothing, not even what the default
-- privileges would hand it.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON user_second_factor, recovery_code, login_challenge TO armature_admin;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        REVOKE ALL ON user_second_factor, recovery_code, login_challenge FROM armature_app;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE org DROP COLUMN IF EXISTS second_factor;
DROP TABLE IF EXISTS login_challenge;
DROP TABLE IF EXISTS recovery_code;
DROP FUNCTION IF EXISTS recovery_code_used_once();
DROP TABLE IF EXISTS user_second_factor;
DROP FUNCTION IF EXISTS second_factor_steps_forward();
