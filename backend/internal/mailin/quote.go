package mailin

import (
	"regexp"
	"strings"
)

// The line a mail client puts above what it quotes. Each client has its own;
// these are the ones a desk actually receives.
var attributionLines = []*regexp.Regexp{
	regexp.MustCompile(`^On .{0,200}wrote:\s*$`),
	regexp.MustCompile(`^Am .{0,200}schrieb .{0,120}:\s*$`),
	regexp.MustCompile(`^Le .{0,200}a écrit\s*:\s*$`),
	regexp.MustCompile(`^-{2,}\s*Original Message\s*-{2,}\s*$`),
	regexp.MustCompile(`^-{2,}\s*Ursprüngliche Nachricht\s*-{2,}\s*$`),
	regexp.MustCompile(`^_{5,}\s*$`),
	regexp.MustCompile(`^--$`),
	regexp.MustCompile(`^Sent from my .{0,40}$`),
}

// fromLine is Outlook's way: a blank line, then the quoted headers.
var fromLine = regexp.MustCompile(`^(From|Von|De):\s.+@.+$`)

// StripQuotes keeps what the sender wrote and drops what their client quoted
// under it: lines beginning with a bracket, and everything from the first
// attribution line down, including one wrapped over two lines.
func StripQuotes(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	cut := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isAttribution(trimmed) || (i > 0 && strings.TrimSpace(lines[i-1]) == "" && fromLine.MatchString(trimmed)) {
			cut = i
			break
		}
		// A wrapped attribution: "On Mon, 1 Sep 2026 at 10:00, Ada Lovelace" then
		// "<ada@example.com> wrote:" on the next line.
		if strings.HasPrefix(trimmed, "On ") && i+1 < len(lines) && strings.HasSuffix(strings.TrimSpace(lines[i+1]), "wrote:") {
			cut = i
			break
		}
	}
	var kept []string
	for _, line := range lines[:cut] {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	}
	out := strings.Join(kept, "\n")
	out = blankRuns.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}

func isAttribution(line string) bool {
	for _, re := range attributionLines {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}
