package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// The seed names the realm, its client, its secret and its groups by hand; a
// renamed secret once went unnoticed until somebody tried to sign in.
func TestTheSeedNamesWhatTheRealmHolds(t *testing.T) {
	path := os.Getenv("ARMATURE_REALM_FILE")
	if path == "" {
		t.Skip("ARMATURE_REALM_FILE is not set; run the suite with `make test`")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	type client struct {
		ClientID string `json:"clientId"`
		Secret   string `json:"secret"`
	}
	var realm struct {
		Realm   string   `json:"realm"`
		Clients []client `json:"clients"`
		Groups  []struct {
			Name string `json:"name"`
		} `json:"groups"`
		Users []struct {
			Email string `json:"email"`
		} `json:"users"`
	}
	if err := json.Unmarshal(raw, &realm); err != nil {
		t.Fatalf("read the realm: %v", err)
	}

	if want := "/realms/" + realm.Realm; !strings.HasSuffix(keycloakIssuer, want) {
		t.Errorf("the seed's issuer %s is not the realm %q; make keycloakIssuer end in %s", keycloakIssuer, realm.Realm, want)
	}
	i := slices.IndexFunc(realm.Clients, func(c client) bool { return c.ClientID == keycloakClientID })
	switch {
	case i < 0:
		t.Errorf("the realm has no client %q; add it to deploy/keycloak/realm.json or change keycloakClientID", keycloakClientID)
	case realm.Clients[i].Secret != keycloakClientSecret:
		t.Errorf("the client %q has the secret %q in the realm and %q in the seed; make keycloakClientSecret match the realm", keycloakClientID, realm.Clients[i].Secret, keycloakClientSecret)
	}

	groups := map[string]bool{}
	for _, g := range realm.Groups {
		groups[g.Name] = true
	}
	for _, g := range realmGroups {
		if !groups[g.group] {
			t.Errorf("the seed gives the group %q a role, but the realm has no such group; add it to deploy/keycloak/realm.json", g.group)
		}
	}
	users := map[string]bool{}
	for _, u := range realm.Users {
		users[strings.ToLower(u.Email)] = true
	}
	for _, u := range realmPeople {
		if !users[u.email] {
			t.Errorf("the seed lets %s in to sign in through the realm, but the realm has nobody with that address; add them there or drop them from realmPeople", u.email)
		}
	}
}
