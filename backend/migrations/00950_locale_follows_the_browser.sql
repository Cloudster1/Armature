-- +goose Up
-- A person who has not chosen a language follows the browser's (#354). The
-- default was 'en', which the profile never offered, so nobody ever did, and
-- every account still holding it was never chosen.

ALTER TABLE app_user ALTER COLUMN locale SET DEFAULT '';
UPDATE app_user SET locale = '' WHERE locale = 'en';

-- +goose Down
UPDATE app_user SET locale = 'en' WHERE locale = '';
ALTER TABLE app_user ALTER COLUMN locale SET DEFAULT 'en';
