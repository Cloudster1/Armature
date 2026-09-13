package issue

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
)

// Member is somebody a comment can name: by their full name or their address,
// each written after an at sign.
type Member struct {
	ID    uuid.UUID
	Name  string
	Email string
}

// ParseMentions finds the members a text names. Names may hold spaces, so the
// text is matched against each member rather than split into words; a longer
// name is tried first so "@Ada Lovelace" is not read as "@Ada".
func ParseMentions(text string, members []Member) []uuid.UUID {
	if !strings.Contains(text, "@") {
		return nil
	}
	lower := strings.ToLower(text)
	sorted := make([]Member, len(members))
	copy(sorted, members)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i].Name) > len(sorted[j].Name) })

	var out []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, m := range sorted {
		if seen[m.ID] {
			continue
		}
		for _, handle := range []string{m.Name, m.Email} {
			handle = strings.ToLower(strings.TrimSpace(handle))
			if handle == "" {
				continue
			}
			if mentionedIn(lower, "@"+handle) {
				out = append(out, m.ID)
				seen[m.ID] = true
				break
			}
		}
	}
	return out
}

// mentionedIn reports whether the handle appears in the text and ends at a
// boundary, so "@Ada" does not match "@Adam".
func mentionedIn(text, handle string) bool {
	for at := 0; ; {
		i := strings.Index(text[at:], handle)
		if i < 0 {
			return false
		}
		end := at + i + len(handle)
		if end == len(text) || !isNameRune(rune(text[end])) {
			return true
		}
		at = end
	}
}

func isNameRune(r rune) bool {
	return r == '_' || r == '.' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127
}

// members lists the organization's people who can be named: everyone with a
// place here who is not a customer.
func (s *Service) members(ctx context.Context, tx db.DBTX) ([]Member, error) {
	rows, err := tx.Query(ctx, `
		SELECT u.id, u.name, u.email FROM app_user u
		JOIN org_member m ON m.user_id = u.id AND m.org_id = current_org_id()
		WHERE u.is_active AND m.org_role <> 'customer'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Name, &m.Email); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
