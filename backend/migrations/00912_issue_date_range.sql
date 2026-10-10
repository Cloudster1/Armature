-- +goose Up
-- A plan reads every day between an issue's ends, so a day typed in the wrong
-- century is refused here as the service refuses it. NOT VALID leaves rows
-- already written alone, and holds every write from now on to the range.
ALTER TABLE issue ADD CONSTRAINT issue_dates_in_range CHECK (
    (start_date IS NULL OR start_date BETWEEN DATE '1900-01-01' AND DATE '2199-12-31')
    AND (due_date IS NULL OR due_date BETWEEN DATE '1900-01-01' AND DATE '2199-12-31')
) NOT VALID;

-- +goose Down
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_dates_in_range;
