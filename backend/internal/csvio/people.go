package csvio

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// PersonChoice is what one name in the file becomes here: a member, or an
// account made for them, or nobody.
type PersonChoice struct {
	Member *uuid.UUID `json:"member,omitempty"`
	Create bool       `json:"create,omitempty"`
}

// People is how the file's names are answered. A name nobody answers for is
// left off the issue and named in the report, never guessed at.
type People struct {
	Domain  string                  `json:"domain,omitempty"`
	Choices map[string]PersonChoice `json:"choices,omitempty"`
}

// found lists the distinct people a file names, with the member each looks
// like, so the person importing confirms rather than types.
func found(rows []Row, mapping Mapping, look *lookups) []PersonFound {
	counts := map[string]int{}
	written := map[string]string{}
	for _, row := range rows {
		for _, target := range []string{"assignee", "reporter"} {
			for _, who := range row.values(mapping, target) {
				key := personKey(who)
				if key == "" {
					continue
				}
				counts[key]++
				if _, seen := written[key]; !seen {
					written[key] = who
				}
			}
		}
	}
	out := make([]PersonFound, 0, len(counts))
	for key, count := range counts {
		person := PersonFound{Name: written[key], Count: count}
		if id, ok := look.people[key]; ok {
			person.Member = &id
			person.Email = look.emails[id]
		}
		out = append(out, person)
	}
	sortByCount(out)
	return out
}

// resolve answers every name the file uses, making the accounts that were
// asked for. A dry run says what it would make and makes nothing.
func (s *Service) resolve(ctx context.Context, people []PersonFound, choice People, dry bool, report *ImportReport) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	for _, person := range people {
		key := personKey(person.Name)
		picked, said := choice.Choices[person.Name]
		if !said {
			picked, said = choice.Choices[key]
		}
		switch {
		case said && picked.Member != nil:
			out[key] = *picked.Member
		case said && picked.Create:
			id, err := s.account(ctx, person.Name, choice.Domain, dry, report)
			if err != nil {
				return nil, err
			}
			if id != uuid.Nil {
				out[key] = id
			}
		case person.Member != nil:
			out[key] = *person.Member
		default:
			note(report, fmt.Sprintf("Nobody here answers to %s, so the issues they were on name nobody.", person.Name))
		}
	}
	return out, nil
}

// account makes the member a name stands for. The address must be free: one
// that already has an account belongs to somebody who did not ask for this.
func (s *Service) account(ctx context.Context, name, domain string, dry bool, report *ImportReport) (uuid.UUID, error) {
	if strings.TrimSpace(domain) == "" {
		return uuid.Nil, fmt.Errorf("say which domain an account for %s is made at", name)
	}
	address := addressFor(name, domain)
	if dry {
		note(report, fmt.Sprintf("%s would be given an account at %s.", name, address))
		return uuid.Nil, nil
	}
	id, err := s.auth.ImportedMember(ctx, address, name)
	if err != nil {
		return uuid.Nil, err
	}
	note(report, fmt.Sprintf("%s was given an account at %s, which cannot sign in until somebody sets it up.", name, address))
	return id, nil
}

// addressFor writes the address a name gets at the operator's domain.
func addressFor(name, domain string) string {
	local := strings.Join(strings.Fields(personKey(name)), ".")
	return local + "@" + strings.TrimPrefix(strings.TrimSpace(domain), "@")
}

// sortByCount puts the people the file leans on at the top of the list.
func sortByCount(people []PersonFound) {
	for i := 1; i < len(people); i++ {
		for j := i; j > 0 && people[j].Count > people[j-1].Count; j-- {
			people[j], people[j-1] = people[j-1], people[j]
		}
	}
}
