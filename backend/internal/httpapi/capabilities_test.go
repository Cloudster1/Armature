package httpapi

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/events"
)

// capabilityName is name@version, as the shared list in Dynamo spells them.
var capabilityName = regexp.MustCompile(`^[a-z][a-z-]*(\.[a-z][a-z-]*)*@[1-9][0-9]*$`)

// A partner offers a feature on seeing its name at /meta, so the list is the
// one docs/integration.md describes, sorted, and nothing else.
func TestMetaReportsTheIntegrationCapabilities(t *testing.T) {
	want := []string{"calendar@1", "issue-embed@1", "oauth.partner-tokens@1", "plan@1", "remote-link-relations@1", "remote-links@1", "reports@1", "theme-export@1", "webhooks.partner@1", "webhooks@1"}
	if got := capabilities(); !slices.Equal(got, want) {
		t.Errorf("capabilities() = %v, want %v", got, want)
	}
}

// A capability promises operations and topics; renaming or dropping one of
// them must fail here, not at the partner that relied on it.
func TestEveryCapabilityRestsOnWhatTheBuildHas(t *testing.T) {
	routes := map[string]bool{}
	for _, r := range Catalog() {
		routes[r.Method+" "+r.Path] = true
	}
	for _, c := range integrationCapabilities {
		if len(c.operations) == 0 {
			t.Errorf("%s names no operation", c.name)
		}
		for _, op := range c.operations {
			if !routes[op] {
				t.Errorf("%s relies on %q, which is not in the operations table", c.name, op)
			}
		}
		for _, topic := range c.topics {
			if !slices.Contains(events.Topics, topic) {
				t.Errorf("%s relies on the topic %q, which events.Topics does not list", c.name, topic)
			}
		}
	}
}

// A partner matches names exactly, and a breaking change raises the number.
func TestCapabilityNamesAreVersionedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range integrationCapabilities {
		if !capabilityName.MatchString(c.name) {
			t.Errorf("%q is not name@version, such as remote-links@1", c.name)
		}
		if seen[c.name] {
			t.Errorf("%q is listed twice", c.name)
		}
		seen[c.name] = true
		if strings.TrimSpace(c.promise) == "" {
			t.Errorf("%s says nothing of what it promises", c.name)
		}
	}
}
