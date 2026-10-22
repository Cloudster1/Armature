package oauth

import (
	"context"
	"strings"

	"github.com/armature/armature/backend/internal/db"
)

// ConnectedTool is another tool of the suite people can switch to, at the
// address of the connected app an administrator registered for it.
type ConnectedTool struct {
	Product string `json:"product"`
	Name    string `json:"name"`
	URL     string `json:"url"`
}

// partnerTools are the tools a connected app can stand for, in the order the
// avatar menu lists them; product is also the tool's client in the shared realm.
var partnerTools = []struct{ product, name string }{
	{"stator", "Stator"},
	{"exciter", "Exciter"},
}

// ConnectedTools is each partner tool with an allowed connected app that has
// an address; when several stand for one tool, the one changed last wins.
func (s *Service) ConnectedTools(ctx context.Context) ([]ConnectedTool, error) {
	var clients []Client
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT name, client_uri, keycloak_client_id, status FROM oauth_client
			WHERE status = 'active' AND client_uri <> ''
			ORDER BY updated_at DESC, created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Client
			if err := rows.Scan(&c.Name, &c.ClientURI, &c.KeycloakClientID, &c.Status); err != nil {
				return err
			}
			clients = append(clients, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return toolsOf(clients), nil
}

// toolsOf matches apps to tools by the realm's client id or by the tool's name,
// taking the first match in the order given.
func toolsOf(clients []Client) []ConnectedTool {
	out := []ConnectedTool{}
	for _, p := range partnerTools {
		for _, c := range clients {
			if c.Status != StatusActive || c.ClientURI == "" {
				continue
			}
			if c.KeycloakClientID == p.product || strings.EqualFold(strings.TrimSpace(c.Name), p.name) {
				out = append(out, ConnectedTool{Product: p.product, Name: p.name, URL: c.ClientURI})
				break
			}
		}
	}
	return out
}
