package issue

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// MaxDocBytes caps a document: a comment or a description is prose, and a
// megabyte of it is a file that belongs in an attachment.
const MaxDocBytes = 1 << 20

// maxDocDepth stops a nest of lists from being a way to exhaust the server.
const maxDocDepth = 32

// allowedNodes and allowedMarks are exactly what the client can show. The
// database never holds a node the renderer would drop, so a document reads
// back whole on every screen.
var allowedNodes = map[string]bool{
	"doc": true, "paragraph": true, "heading": true, "bulletList": true, "orderedList": true,
	"listItem": true, "blockquote": true, "codeBlock": true, "hardBreak": true, "text": true, "mention": true,
}

var allowedMarks = map[string]bool{"bold": true, "italic": true, "code": true, "strike": true, "link": true}

// docNode is the shape every node shares; attrs differ by type.
type docNode struct {
	Type    string            `json:"type"`
	Text    string            `json:"text"`
	Attrs   map[string]any    `json:"attrs"`
	Marks   []docMark         `json:"marks"`
	Content []json.RawMessage `json:"content"`
}

type docMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// ValidateDocument refuses anything the renderer cannot show, in a sentence
// that names the noun (a comment, a description) and what to do about it.
func ValidateDocument(body json.RawMessage, noun string) error {
	if len(body) > MaxDocBytes {
		return fmt.Errorf("That %s is too long; keep it under 1 MB, or attach a file.", noun)
	}
	var root docNode
	if err := json.Unmarshal(body, &root); err != nil {
		return fmt.Errorf("The %s is not a document this tracker can read.", noun)
	}
	if root.Type != "doc" {
		return fmt.Errorf("The %s must be a document with \"type\":\"doc\".", noun)
	}
	return validateNode(root, noun, 0)
}

func validateNode(n docNode, noun string, depth int) error {
	if depth > maxDocDepth {
		return fmt.Errorf("The %s is nested too deeply; flatten its lists.", noun)
	}
	if !allowedNodes[n.Type] {
		return fmt.Errorf("The %s holds a %q, which this tracker cannot show; take it out.", noun, n.Type)
	}
	switch n.Type {
	case "text":
		if n.Text == "" {
			return fmt.Errorf("The %s holds an empty text node.", noun)
		}
	case "heading":
		level, ok := numberAttr(n.Attrs, "level")
		if !ok || level < 1 || level > 3 {
			return fmt.Errorf("The %s has a heading beyond level 3; use levels 1 to 3.", noun)
		}
	case "mention":
		id, _ := n.Attrs["id"].(string)
		label, _ := n.Attrs["label"].(string)
		if _, err := uuid.Parse(id); err != nil || strings.TrimSpace(label) == "" {
			return fmt.Errorf("The %s mentions somebody without saying who; pick them from the list.", noun)
		}
	}
	for _, m := range n.Marks {
		if !allowedMarks[m.Type] {
			return fmt.Errorf("The %s uses a %q style, which this tracker cannot show; take it out.", noun, m.Type)
		}
		if m.Type == "link" {
			href, _ := m.Attrs["href"].(string)
			if !SafeHref(href) {
				return fmt.Errorf("The %s links to %q, which is not a web or mail address.", noun, href)
			}
		}
	}
	for _, raw := range n.Content {
		var child docNode
		if err := json.Unmarshal(raw, &child); err != nil {
			return fmt.Errorf("The %s is not a document this tracker can read.", noun)
		}
		if err := validateNode(child, noun, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func numberAttr(attrs map[string]any, key string) (int, bool) {
	switch v := attrs[key].(type) {
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	}
	return 0, false
}

// SafeHref admits a web or a mail address and nothing that runs.
func SafeHref(href string) bool {
	lower := strings.ToLower(strings.TrimSpace(href))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:")
}

// IsEmptyDocument reports whether a document says nothing: no content, or
// only paragraphs with nothing in them.
func IsEmptyDocument(body json.RawMessage) bool {
	if len(body) == 0 || string(body) == "null" {
		return true
	}
	var root docNode
	if err := json.Unmarshal(body, &root); err != nil {
		return false
	}
	for _, raw := range root.Content {
		var child docNode
		if err := json.Unmarshal(raw, &child); err != nil {
			return false
		}
		if child.Type != "paragraph" || len(child.Content) > 0 {
			return false
		}
	}
	return true
}

// MentionIDs lists the people a document names as mention nodes.
func MentionIDs(body json.RawMessage) []uuid.UUID {
	out := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	var walk func(raw json.RawMessage)
	walk = func(raw json.RawMessage) {
		var n docNode
		if err := json.Unmarshal(raw, &n); err != nil {
			return
		}
		if n.Type == "mention" {
			if id, err := uuid.Parse(fmt.Sprint(n.Attrs["id"])); err == nil && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(body)
	return out
}

// PlainText renders a document down to text the way a person would type it:
// a mention as @Name, a list as lines with dashes or numbers, a quote with
// its bars, code between fences. It serves mail, search and every place a
// document cannot be shown.
func PlainText(doc json.RawMessage) string {
	var b strings.Builder
	renderBlocks(&b, doc, "", 0)
	return strings.TrimSpace(b.String())
}

// renderBlocks writes a node's children as blocks; prefix opens every line of
// a quote, and ordered lists count from one.
func renderBlocks(b *strings.Builder, raw json.RawMessage, prefix string, depth int) {
	var n docNode
	if err := json.Unmarshal(raw, &n); err != nil || depth > maxDocDepth {
		return
	}
	switch n.Type {
	case "doc":
		for i, child := range n.Content {
			if i > 0 {
				b.WriteString("\n")
			}
			renderBlocks(b, child, prefix, depth+1)
		}
	case "paragraph", "heading":
		b.WriteString(prefix + strings.ReplaceAll(inlineText(n), "\n", "\n"+prefix) + "\n")
	case "blockquote":
		for _, child := range n.Content {
			renderBlocks(b, child, prefix+"> ", depth+1)
		}
	case "codeBlock":
		b.WriteString(prefix + "```\n" + prefix + strings.ReplaceAll(inlineText(n), "\n", "\n"+prefix) + "\n" + prefix + "```\n")
	case "bulletList", "orderedList":
		for i, child := range n.Content {
			marker := "- "
			if n.Type == "orderedList" {
				marker = strconv.Itoa(i+1) + ". "
			}
			var item docNode
			if err := json.Unmarshal(child, &item); err != nil {
				continue
			}
			for j, block := range item.Content {
				var inner strings.Builder
				renderBlocks(&inner, block, "", depth+1)
				lines := strings.Split(strings.TrimRight(inner.String(), "\n"), "\n")
				for k, line := range lines {
					lead := strings.Repeat(" ", len(marker))
					if j == 0 && k == 0 {
						lead = marker
					}
					b.WriteString(prefix + lead + line + "\n")
				}
			}
		}
	default:
		if text := inlineText(n); text != "" {
			b.WriteString(prefix + text + "\n")
		}
	}
}

// inlineText flattens a block's inline children: text, mentions and breaks.
func inlineText(n docNode) string {
	var b strings.Builder
	var walk func(raw json.RawMessage)
	walk = func(raw json.RawMessage) {
		var c docNode
		if err := json.Unmarshal(raw, &c); err != nil {
			return
		}
		switch c.Type {
		case "text":
			b.WriteString(c.Text)
		case "mention":
			b.WriteString("@" + fmt.Sprint(c.Attrs["label"]))
		case "hardBreak":
			b.WriteString("\n")
		default:
			for _, child := range c.Content {
				walk(child)
			}
		}
	}
	for _, child := range n.Content {
		walk(child)
	}
	return b.String()
}

// ErrEmptyDocument is what a comment with nothing in it is refused with.
var ErrEmptyDocument = errors.New("a comment cannot be empty")
