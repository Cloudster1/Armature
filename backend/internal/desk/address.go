package desk

import "strings"

// Each desk is a plus address of the one inbox, support+cp@acme.test for
// support@acme.test; mail hosts deliver those to the inbox, so one box serves all.

// Address is a desk's own address, or empty when there is no inbox.
func Address(inbox, projectKey string) string {
	local, domain, ok := strings.Cut(strings.ToLower(bareAddress(inbox)), "@")
	if !ok || local == "" || domain == "" || projectKey == "" {
		return ""
	}
	return local + "+" + strings.ToLower(projectKey) + "@" + domain
}

// deskKeyIn says which desk a recipient names: the key after the plus, upper
// cased as keys are. The bare inbox and any other address name none.
func deskKeyIn(inbox, recipient string) string {
	local, domain, ok := strings.Cut(inbox, "@")
	if !ok {
		return ""
	}
	tag, ok := strings.CutPrefix(recipient, local+"+")
	if !ok {
		return ""
	}
	key, ok := strings.CutSuffix(tag, "@"+domain)
	if !ok || key == "" || strings.ContainsAny(key, "@+") {
		return ""
	}
	return strings.ToUpper(key)
}
