package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// A desk's companies: customers who read the requests a colleague shared with
// them. Who is in one is the desk's to say; what is shared, the reporter's.

// MaxCompanyName bounds a company's name, as the table does.
const MaxCompanyName = 100

var (
	// ErrCompanyNotFound is returned for a company that is not there.
	ErrCompanyNotFound = errors.New("company not found")
	// ErrCompanyNameTaken is returned when the desk already has a company by that name.
	ErrCompanyNameTaken = errors.New("this desk already has a company by that name")
	// ErrCompanyName is returned for a company with no name, or too long a one.
	ErrCompanyName = errors.New("a company needs a name of at most 100 characters")
	// ErrNoCompany is returned when a request is to be shared by somebody who
	// is in no company at the desk.
	ErrNoCompany = errors.New("you are in no company at this desk, so there is nobody to share the request with")
	// ErrMemberNotACustomer is returned when somebody who works here is added
	// to a company; they see every request already.
	ErrMemberNotACustomer = errors.New("that address belongs to somebody who works here, and a company is for customers")
	// ErrInAnotherCompany is returned when the customer is in another company of the desk.
	ErrInAnotherCompany = errors.New("that customer is in another company of this desk")
	// ErrNotAMember is returned when a removal names somebody not in the company.
	ErrNotAMember = errors.New("that customer is not in this company")
)

// DomainTakenError names the company that already takes a domain, since a
// customer at it could join only one.
type DomainTakenError struct {
	Domain, Company string
}

func (e *DomainTakenError) Error() string {
	return fmt.Sprintf("%s already takes %s", e.Company, e.Domain)
}

// Company is a desk's group of customers.
type Company struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"projectId"`
	ProjectKey string    `json:"projectKey"`
	Name       string    `json:"name"`
	// Domains are where a customer joins from on their first request here.
	Domains   []string        `json:"domains"`
	Members   []CompanyMember `json:"members"`
	CreatedAt time.Time       `json:"createdAt"`
}

// CompanyMember is one customer in a company.
type CompanyMember struct {
	UserID  uuid.UUID `json:"userId"`
	Name    string    `json:"name"`
	Email   string    `json:"email"`
	AddedAt time.Time `json:"addedAt"`
}

// CompanyInput is a company as the form sends it; nil leaves a field alone.
type CompanyInput struct {
	Name    *string
	Domains *[]string
}

const selectCompany = `
SELECT c.id, c.project_id, p.key, c.name, c.domains, c.created_at,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('userId', u.id, 'name', u.name, 'email', u.email::text, 'addedAt', m.added_at)
                                  ORDER BY lower(u.name), u.email)
                 FROM customer_company_member m JOIN app_user u ON u.id = m.user_id WHERE m.company_id = c.id), '[]'::jsonb)
FROM customer_company c
JOIN project p ON p.id = c.project_id`

func scanCompany(row pgx.Row) (*Company, error) {
	var (
		c       Company
		members []byte
	)
	err := row.Scan(&c.ID, &c.ProjectID, &c.ProjectKey, &c.Name, &c.Domains, &c.CreatedAt, &members)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCompanyNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.Domains == nil {
		c.Domains = []string{}
	}
	c.Members = []CompanyMember{}
	if err := json.Unmarshal(members, &c.Members); err != nil {
		return nil, fmt.Errorf("read the company's members: %w", err)
	}
	return &c, nil
}

// Companies lists a desk's companies by name, each with its members.
func (s *Service) Companies(ctx context.Context, projectKey string) ([]Company, error) {
	out := []Company{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectCompany+` WHERE p.key = $1 ORDER BY lower(c.name)`, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCompany(rows)
			if err != nil {
				return err
			}
			out = append(out, *c)
		}
		return rows.Err()
	})
	return out, err
}

// companyName trims a name and holds it to what the table takes.
func companyName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || len([]rune(name)) > MaxCompanyName {
		return "", ErrCompanyName
	}
	return name, nil
}

// domainsFree refuses a domain another company of the desk already takes. The
// trigger refuses it too; this names the company.
func domainsFree(ctx context.Context, tx db.DBTX, projectID, self uuid.UUID, domains []string) error {
	var domain, company string
	err := tx.QueryRow(ctx, `
		SELECT d, c.name FROM customer_company c, unnest(c.domains) AS d
		WHERE c.project_id = $1 AND c.id <> $2 AND d = ANY($3)
		ORDER BY d LIMIT 1`, projectID, self, domains).Scan(&domain, &company)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &DomainTakenError{Domain: domain, Company: company}
}

// CreateCompany adds a company to a desk.
func (s *Service) CreateCompany(ctx context.Context, projectKey string, in CompanyInput) (*Company, db.LSN, error) {
	name, err := companyName(deref(in.Name))
	if err != nil {
		return nil, 0, err
	}
	domains := []string{}
	if in.Domains != nil {
		if domains, err = project.NormalizeDomains(*in.Domains); err != nil {
			return nil, 0, err
		}
	}
	var out *Company
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		projectID, err := s.serviceProject(ctx, tx, projectKey)
		if err != nil {
			return err
		}
		if err := domainsFree(ctx, tx, projectID, uuid.Nil, domains); err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `INSERT INTO customer_company (project_id, name, domains) VALUES ($1, $2, $3) RETURNING id`,
			projectID, name, domains).Scan(&id)
		if isUniqueViolation(err) {
			return ErrCompanyNameTaken
		}
		if err != nil {
			return fmt.Errorf("create company: %w", err)
		}
		out, err = scanCompany(tx.QueryRow(ctx, selectCompany+` WHERE c.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// UpdateCompany renames a company or changes its domains. Who is in it stays:
// a domain lets new customers in, and taking one away sends nobody out.
func (s *Service) UpdateCompany(ctx context.Context, id uuid.UUID, in CompanyInput) (*Company, db.LSN, error) {
	var out *Company
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanCompany(tx.QueryRow(ctx, selectCompany+` WHERE c.id = $1 FOR UPDATE OF c`, id))
		if err != nil {
			return err
		}
		name, domains := before.Name, before.Domains
		if in.Name != nil {
			if name, err = companyName(*in.Name); err != nil {
				return err
			}
		}
		if in.Domains != nil {
			if domains, err = project.NormalizeDomains(*in.Domains); err != nil {
				return err
			}
			if err := domainsFree(ctx, tx, before.ProjectID, id, domains); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE customer_company SET name = $2, domains = $3 WHERE id = $1`, id, name, domains)
		if isUniqueViolation(err) {
			return ErrCompanyNameTaken
		}
		if err != nil {
			return fmt.Errorf("update company: %w", err)
		}
		out, err = scanCompany(tx.QueryRow(ctx, selectCompany+` WHERE c.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// DeleteCompany removes a company. Its requests stay with their reporters,
// and what was shared with it is no longer shared with anybody.
func (s *Service) DeleteCompany(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM customer_company WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrCompanyNotFound
		}
		return nil
	})
}

// AddCompanyMember puts a customer in a company. Somebody with an account and
// no standing becomes a customer, as a mailed code would make them.
func (s *Service) AddCompanyMember(ctx context.Context, id, userID uuid.UUID) (*Company, db.LSN, error) {
	var out *Company
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		company, err := scanCompany(tx.QueryRow(ctx, selectCompany+` WHERE c.id = $1`, id))
		if err != nil {
			return err
		}
		var standing *string
		err = tx.QueryRow(ctx, `
			UPDATE app_user SET standing = COALESCE(standing, 'customer') WHERE id = $1 AND erased_at IS NULL
			RETURNING standing`, userID).Scan(&standing)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAMember
		}
		if err != nil {
			return err
		}
		if standing == nil || *standing != "customer" {
			return ErrMemberNotACustomer
		}
		var elsewhere *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT company_id FROM customer_company_member WHERE project_id = $1 AND user_id = $2`,
			company.ProjectID, userID).Scan(&elsewhere); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if elsewhere != nil && *elsewhere != id {
			return ErrInAnotherCompany
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO customer_company_member (company_id, project_id, user_id) VALUES ($1, $2, $3)
			ON CONFLICT (company_id, user_id) DO NOTHING`, id, company.ProjectID, userID); err != nil {
			if isUniqueViolation(err) {
				return ErrInAnotherCompany
			}
			return fmt.Errorf("add to the company: %w", err)
		}
		out, err = scanCompany(tx.QueryRow(ctx, selectCompany+` WHERE c.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// RemoveCompanyMember takes a customer out of a company. What they shared stays
// with it, and what colleagues shared they stop seeing at once.
func (s *Service) RemoveCompanyMember(ctx context.Context, id, userID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customer_company WHERE id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrCompanyNotFound
		}
		tag, err := tx.Exec(ctx, `DELETE FROM customer_company_member WHERE company_id = $1 AND user_id = $2`, id, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotAMember
		}
		return nil
	})
}

// companyAt is a customer's company at a desk. On their first request there,
// the company that takes their domain counts, and join puts them in it.
func companyAt(ctx context.Context, tx db.DBTX, projectID, userID uuid.UUID, join bool) (*issue.CompanyRef, error) {
	var ref issue.CompanyRef
	err := tx.QueryRow(ctx, `
		SELECT c.id, c.name FROM customer_company_member m JOIN customer_company c ON c.id = m.company_id
		WHERE m.project_id = $1 AND m.user_id = $2`, projectID, userID).Scan(&ref.ID, &ref.Name)
	if err == nil {
		return &ref, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	err = tx.QueryRow(ctx, `
		SELECT c.id, c.name FROM customer_company c, app_user u
		WHERE c.project_id = $1 AND u.id = $2 AND u.standing = 'customer'
		  AND lower(split_part(u.email::text, '@', 2)) = ANY(c.domains)
		  AND NOT EXISTS (SELECT 1 FROM issue i WHERE i.project_id = $1 AND i.reporter_id = $2)`,
		projectID, userID).Scan(&ref.ID, &ref.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if join {
		if _, err := tx.Exec(ctx, `
			INSERT INTO customer_company_member (company_id, project_id, user_id) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, ref.ID, projectID, userID); err != nil {
			return nil, fmt.Errorf("join the company: %w", err)
		}
	}
	return &ref, nil
}

// raisersCompany is the company a request is raised for. An open door took the
// address on trust, so through one a customer has no company to speak for.
func (s *Service) raisersCompany(ctx context.Context, projectID uuid.UUID, customer issue.Actor) (*issue.CompanyRef, error) {
	if _, open := ScopeFrom(ctx); open || !IsCustomer(customer.OrgRole) {
		return nil, nil
	}
	var out *issue.CompanyRef
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = companyAt(ctx, tx, projectID, customer.UserID, true)
		return err
	})
	return out, err
}

// sharedWith says whether a request was shared with a company the customer is
// in. Membership is read now, so somebody who left stops seeing at once.
func (s *Service) sharedWith(ctx context.Context, found *issue.Issue, customer uuid.UUID) (bool, error) {
	if _, open := ScopeFrom(ctx); open || !found.SharedWithCompany || found.Company == nil {
		return false, nil
	}
	var member bool
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customer_company_member WHERE company_id = $1 AND user_id = $2)`,
			found.Company.ID, customer).Scan(&member)
	})
	return member, err
}

// SharedRequests lists what colleagues shared with the customer's companies,
// newest first, less what the customer raised or follows themselves.
func (s *Service) SharedRequests(ctx context.Context, customer uuid.UUID) ([]issue.Issue, error) {
	if _, open := ScopeFrom(ctx); open {
		return []issue.Issue{}, nil
	}
	const pageSize = 100
	page, err := s.issues.List(ctx, issue.Filter{SharedWith: &customer, ProjectKind: string(project.KindService)},
		issue.Page{Limit: pageSize, OrderBy: "created", Desc: true})
	if err != nil {
		return nil, err
	}
	if page.Issues == nil {
		return []issue.Issue{}, nil
	}
	return page.Issues, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
