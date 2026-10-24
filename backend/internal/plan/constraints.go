package plan

import "github.com/armature/armature/backend/internal/issue"

// Constraint is one issue that has to start after another finishes, with the
// Blocks link it comes from: theirs, or a parent's above them.
type Constraint struct {
	BlockerKey, BlockedKey string
	Via                    issue.Blocker
}

// Direct says the constraint is the link itself, not one a parent passed down.
func (c Constraint) Direct() bool {
	return c.Via.BlockerKey == c.BlockerKey && c.Via.BlockedKey == c.BlockedKey
}

// Constraints spreads each Blocks link over everything beneath its two ends, except a link
// within one line of parents, where spreading would make work wait on itself.
func Constraints(items []Item, blockers []issue.Blocker) []Constraint {
	beneath := map[string][]string{}
	derived := map[string]bool{}
	var walk func(item Item) []string
	walk = func(item Item) []string {
		var all []string
		for _, child := range item.Children {
			all = append(all, child.Issue.Key)
			all = append(all, walk(child)...)
		}
		beneath[item.Issue.Key] = all
		derived[item.Issue.Key] = item.Derived
		return all
	}
	for _, item := range items {
		walk(item)
	}
	within := func(key, ancestor string) bool {
		for _, k := range beneath[ancestor] {
			if k == key {
				return true
			}
		}
		return false
	}
	side := func(key string) []string {
		out := []string{}
		for _, k := range append([]string{key}, beneath[key]...) {
			if !derived[k] {
				out = append(out, k)
			}
		}
		return out
	}

	var out []Constraint
	seen := map[[2]string]bool{}
	add := func(c Constraint) {
		pair := [2]string{c.BlockerKey, c.BlockedKey}
		if c.BlockerKey == c.BlockedKey || seen[pair] {
			return
		}
		seen[pair] = true
		out = append(out, c)
	}
	for _, b := range blockers {
		if within(b.BlockedKey, b.BlockerKey) || within(b.BlockerKey, b.BlockedKey) {
			add(Constraint{BlockerKey: b.BlockerKey, BlockedKey: b.BlockedKey, Via: b})
			continue
		}
		for _, from := range side(b.BlockerKey) {
			for _, to := range side(b.BlockedKey) {
				add(Constraint{BlockerKey: from, BlockedKey: to, Via: b})
			}
		}
	}
	return out
}
