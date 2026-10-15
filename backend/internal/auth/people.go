package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/installation"
)

// DefaultPeoplePage is a page of people unless asked otherwise; MaxPeoplePage
// is the most one read returns, so thousands of accounts never travel at once.
const (
	DefaultPeoplePage = 50
	MaxPeoplePage     = 200
)

// PeopleFilter narrows and pages a list of people.
type PeopleFilter struct {
	// Text matches part of the name or the address; names it starts come first.
	Text string
	// Staff leaves the service desk's customers out.
	Staff  bool
	Limit  int
	Offset int
}

// Member is a person as the pickers see them.
type Member struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	AvatarURL string    `json:"avatarUrl,omitempty"`
}

// MemberPage is one page of members plus how many matched in all.
type MemberPage struct {
	Members []Member `json:"members"`
	Total   int      `json:"total"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

// UserPage is one page of the users page's accounts plus how many matched in all.
type UserPage struct {
	Users  []ManagedUser `json:"users"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

func (f PeopleFilter) bounded() PeopleFilter {
	if f.Limit <= 0 || f.Limit > MaxPeoplePage {
		f.Limit = DefaultPeoplePage
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	f.Text = strings.TrimSpace(f.Text)
	return f
}

// narrow adds the filter's clauses after args, and the order of the matches.
// strpos rather than LIKE, so a percent sign or an underscore is only text.
func (f PeopleFilter) narrow(args []any) (where, order string, _ []any) {
	order = ` ORDER BY u.name, u.email, u.id`
	if f.Staff {
		where += ` AND u.standing <> 'customer'`
	}
	if f.Text != "" {
		args = append(args, f.Text)
		n := len(args)
		where += fmt.Sprintf(` AND (strpos(lower(u.name), lower($%d)) > 0 OR strpos(lower(u.email::text), lower($%d)) > 0)`, n, n)
		order = fmt.Sprintf(` ORDER BY strpos(lower(u.name), lower($%d)) <> 1, u.name, u.email, u.id`, n)
	}
	return where, order, args
}

// Members lists one page of the people who may be picked: active, with a standing.
func (s *Service) Members(ctx context.Context, f PeopleFilter) (MemberPage, error) {
	f = f.bounded()
	page := MemberPage{Members: []Member{}, Limit: f.Limit, Offset: f.Offset}
	where, order, args := f.narrow(nil)
	from := ` FROM app_user u WHERE u.is_active AND u.standing IS NOT NULL` + where
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*)`+from, args...).Scan(&page.Total); err != nil {
			return err
		}
		n := len(args)
		rows, err := tx.Query(ctx, `SELECT u.id, u.name, u.email, u.standing, COALESCE(u.avatar_url, '')`+from+order+
			fmt.Sprintf(` LIMIT $%d OFFSET $%d`, n+1, n+2), append(args, f.Limit, f.Offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Member
			if err := rows.Scan(&m.ID, &m.Name, &m.Email, &m.Role, &m.AvatarURL); err != nil {
				return err
			}
			page.Members = append(page.Members, m)
		}
		return rows.Err()
	})
	if err != nil {
		return MemberPage{}, fmt.Errorf("list members: %w", err)
	}
	return page, nil
}

// ManagedUsers lists one page of the installation's people, switched off ones
// included, with what an administrator may do to each.
func (s *Service) ManagedUsers(ctx context.Context, actorRole OrgRole, f PeopleFilter) (UserPage, error) {
	f = f.bounded()
	page := UserPage{Users: []ManagedUser{}, Limit: f.Limit, Offset: f.Offset}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		automation, err := installation.AutomationUser(ctx, tx)
		if err != nil {
			return err
		}
		where, order, args := f.narrow([]any{automation})
		if err := tx.QueryRow(ctx, `SELECT count(*)`+managedFromSQL+where, args...).
			Scan(&page.Total); err != nil {
			return err
		}
		n := len(args)
		rows, err := tx.Query(ctx, managedSelectSQL+where+order+fmt.Sprintf(` LIMIT $%d OFFSET $%d`, n+1, n+2), append(args, f.Limit, f.Offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanManaged(rows, actorRole)
			if err != nil {
				return err
			}
			page.Users = append(page.Users, r.user)
		}
		return rows.Err()
	})
	if err != nil {
		return UserPage{}, fmt.Errorf("list users: %w", err)
	}
	return page, nil
}
