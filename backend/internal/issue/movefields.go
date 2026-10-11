package issue

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
)

// FieldValueText reads a stored custom field value the way the field shows it.
// It lives here because the field package depends on this one, not the reverse.
func FieldValueText(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "Yes"
		}
		return ""
	}
	return string(raw)
}

// dropProjectFieldValues removes an issue's answers to project fields, which
// mean nothing elsewhere, and says what each was. Organization fields stay.
func dropProjectFieldValues(ctx context.Context, tx db.DBTX, issueID uuid.UUID) ([]Change, error) {
	rows, err := tx.Query(ctx, `
		WITH gone AS (
			DELETE FROM issue_field_value v USING custom_field f
			WHERE v.field_id = f.id AND v.issue_id = $1 AND f.project_id IS NOT NULL
			RETURNING f.name, f.position, f.created_at, v.value
		)
		SELECT name, value FROM gone ORDER BY position, created_at`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var changes []Change
	for rows.Next() {
		var (
			name string
			raw  []byte
		)
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		changes = append(changes, Change{Field: name, From: FieldValueText(raw)})
	}
	return changes, rows.Err()
}
