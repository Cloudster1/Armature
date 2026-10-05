-- +goose Up
-- Which days somebody can work: named calendars of days off, and a working
-- week per person saying how many minutes each weekday holds.

CREATE TABLE holiday_calendar (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name       text NOT NULL,
    -- The calendar for everybody who has not been given another.
    is_default boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- The same bounds the service checks.
    CONSTRAINT holiday_calendar_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 100)
);

CREATE UNIQUE INDEX holiday_calendar_name_idx ON holiday_calendar (org_id, lower(name));
CREATE UNIQUE INDEX holiday_calendar_one_default_idx ON holiday_calendar (org_id) WHERE is_default;

CREATE TRIGGER holiday_calendar_set_updated_at BEFORE UPDATE ON holiday_calendar
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Every organization that already exists gets the empty default a new one is
-- made with. Written before the policies below, which would hide every org.
INSERT INTO holiday_calendar (org_id, name, is_default)
SELECT id, 'Holidays', true FROM org;

CREATE TABLE holiday (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    calendar_id uuid NOT NULL REFERENCES holiday_calendar(id) ON DELETE CASCADE,
    day         date NOT NULL,
    name        text NOT NULL,
    -- Half a day off: the person works half of what their week says.
    half_day    boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT holiday_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 100)
);

CREATE UNIQUE INDEX holiday_calendar_day_idx ON holiday (calendar_id, day);

-- The policy checks the row's own organization; this checks the calendar is in it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION holiday_same_org() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM holiday_calendar WHERE id = NEW.calendar_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'a holiday must belong to the same organization as its calendar'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER holiday_same_org
    BEFORE INSERT OR UPDATE ON holiday
    FOR EACH ROW EXECUTE FUNCTION holiday_same_org();

-- A working week is an object of weekday to minutes, mon to sun, each a whole
-- number from 0 to a full day. A day left out is a day not worked.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION working_week_is_valid(week jsonb) RETURNS boolean AS $$
    -- CASE rather than AND: jsonb_each raises on anything but an object.
    SELECT CASE WHEN jsonb_typeof(week) <> 'object' THEN false ELSE NOT EXISTS (
        SELECT 1 FROM jsonb_each(week) AS e(day, minutes)
        WHERE CASE
            WHEN e.day NOT IN ('mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun') THEN true
            WHEN jsonb_typeof(e.minutes) <> 'number' THEN true
            ELSE e.minutes::numeric <> trunc(e.minutes::numeric)
                 OR e.minutes::numeric < 0 OR e.minutes::numeric > 1440
        END
    ) END
$$ LANGUAGE sql IMMUTABLE;
-- +goose StatementEnd

-- A person's week here. No row is the standard week on the default calendar.
CREATE TABLE member_schedule (
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    -- Null follows the organization's default, whichever calendar that is.
    calendar_id uuid REFERENCES holiday_calendar(id) ON DELETE SET NULL,
    minutes     jsonb NOT NULL DEFAULT '{"mon": 480, "tue": 480, "wed": 480, "thu": 480, "fri": 480, "sat": 0, "sun": 0}',
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id),
    CONSTRAINT member_schedule_minutes_shape CHECK (working_week_is_valid(minutes))
);

CREATE TRIGGER member_schedule_set_updated_at BEFORE UPDATE ON member_schedule
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Only somebody who works here has a working week: a portal customer does not.
-- A deleted calendar handing people back to the default is let through as is.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION member_schedule_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.calendar_id IS NULL AND OLD.calendar_id IS NOT NULL
       AND NEW.org_id = OLD.org_id AND NEW.user_id = OLD.user_id AND NEW.minutes = OLD.minutes THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM org_member
         WHERE org_id = NEW.org_id AND user_id = NEW.user_id
           AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member of the organization has a working week in it'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.calendar_id IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM holiday_calendar WHERE id = NEW.calendar_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'a working week must keep a calendar of its own organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER member_schedule_check
    BEFORE INSERT OR UPDATE ON member_schedule
    FOR EACH ROW EXECUTE FUNCTION member_schedule_guard();

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['holiday_calendar', 'holiday', 'member_schedule']
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

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON holiday_calendar, holiday, member_schedule
            TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS member_schedule, holiday, holiday_calendar CASCADE;
DROP FUNCTION IF EXISTS member_schedule_guard();
DROP FUNCTION IF EXISTS working_week_is_valid(jsonb);
DROP FUNCTION IF EXISTS holiday_same_org();
