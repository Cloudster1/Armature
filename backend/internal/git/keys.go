package git

import (
	"regexp"
	"strings"
	"unicode"
)

// keyPattern matches an issue key such as CP-142. The project key shape is the
// one the database enforces, so a match is at least a possible key.
var keyPattern = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,9}-[1-9][0-9]*)\b`)

// IssueKeys returns the issue keys mentioned in a piece of text, in order of
// first appearance and without repeats.
func IssueKeys(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range keyPattern.FindAllString(text, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// Command is one instruction a commit message carries for the tracker.
//
// The syntax is the one people already know from Jira: a hash word after an
// issue key. "#start-progress" names a transition by its name with the spaces
// hyphenated; "#comment" takes the rest of the line as a comment.
type Command struct {
	// Transition is the hyphenated, lower case name of a transition to take.
	// Empty for a comment.
	Transition string
	Comment    string
}

// SmartCommands reads the commands out of a commit message.
//
// Commands apply to the whole message rather than to the key they follow, which
// is the simpler rule and the one nearly every real message satisfies: a commit
// is about one issue, and when it is about two they move together.
func SmartCommands(message string) []Command {
	var out []Command
	for _, line := range strings.Split(message, "\n") {
		words := strings.Fields(line)
		for i, word := range words {
			if !strings.HasPrefix(word, "#") || len(word) < 2 {
				continue
			}
			name := strings.ToLower(word[1:])
			if name == "comment" {
				rest := strings.Join(words[i+1:], " ")
				if rest != "" {
					out = append(out, Command{Comment: rest})
				}
				break
			}
			if isCommandWord(name) {
				out = append(out, Command{Transition: name})
			}
		}
	}
	return out
}

// isCommandWord keeps "#start-progress" and drops "#123", which on every host
// is a reference to something else.
func isCommandWord(name string) bool {
	for _, r := range name {
		if !unicode.IsLetter(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

// TransitionSlug is how a transition's name reads as a command: lower case,
// spaces hyphenated. "Start progress" matches "#start-progress".
func TransitionSlug(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), "-"))
}

// BranchName suggests a branch for an issue: its key, then its summary made
// safe for a ref and cut short enough to type, at a word rather than inside one.
func BranchName(issueKey, summary string) string {
	const maxSlug = 40
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(summary) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > maxSlug {
		cut := strings.LastIndex(slug[:maxSlug+1], "-")
		if cut < maxSlug/2 {
			cut = maxSlug
		}
		slug = strings.TrimRight(slug[:cut], "-")
	}
	if slug == "" {
		return issueKey
	}
	return issueKey + "-" + slug
}

// ValidBranchName applies the rules git itself applies to a ref name, so that a
// name the host would refuse is refused here with a reason instead.
func ValidBranchName(name string) bool {
	if name == "" || len(name) > 200 || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") ||
		strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") ||
		strings.Contains(name, "..") || strings.Contains(name, "//") || strings.Contains(name, "@{") || name == "@" {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\", r) {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}
