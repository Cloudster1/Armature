package issue

import (
	"encoding/json"
	"regexp"
	"strings"
)

// maxWikiHeading is the deepest heading a document can show; Jira writes six.
const maxWikiHeading = 3

// WikiDocument reads another tracker's markup as far as this document allows.
// What it does not know it keeps as the text it was, so nothing is ever lost.
func WikiDocument(text string) json.RawMessage {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	r := &wikiReader{}
	r.read(text)
	blocks := r.done()
	if len(blocks) == 0 {
		return nil
	}
	raw, err := json.Marshal(map[string]any{"type": "doc", "content": blocks})
	if err != nil {
		return TextDocument(text)
	}
	return raw
}

// wikiReader turns lines into blocks, holding the paragraph and the lists it
// is in the middle of.
type wikiReader struct {
	blocks []any
	para   []string
	lists  []map[string]any
	code   []string
	fenced bool
}

var (
	wikiHeading = regexp.MustCompile(`^h([1-6])\.\s*(.*)$`)
	wikiItem    = regexp.MustCompile(`^([*#-]+)\s+(.*)$`)
	wikiFence   = regexp.MustCompile(`^\{(code|noformat)`)
	wikiRow     = regexp.MustCompile(`^\s*\|`)
)

func (w *wikiReader) read(text string) {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		// Every block is recognised by what the line says, not by where it
		// starts: an exported list is indented by a space.
		body := strings.TrimSpace(line)
		switch {
		case wikiFence.MatchString(body) || body == "{quote}":
			w.fence()
		case w.fenced:
			w.code = append(w.code, line)
		case body == "":
			w.flush()
		case strings.HasPrefix(body, "----"):
			// A rule is not a node this can show, and its job was to separate.
			w.flush()
		case wikiHeading.MatchString(body):
			w.heading(body)
		case strings.HasPrefix(body, "bq. "):
			w.quote(strings.TrimPrefix(body, "bq. "))
		case wikiItem.MatchString(body):
			parts := wikiItem.FindStringSubmatch(body)
			w.item(parts[1], parts[2])
		case wikiRow.MatchString(body):
			// A table flattens to its cells: the row is the meaning, the ruling
			// was the tracker's.
			w.line(strings.Join(cells(body), " | "))
		default:
			w.line(body)
		}
	}
}

// done closes whatever the last line left open.
func (w *wikiReader) done() []any {
	if w.fenced {
		w.fence()
	}
	w.flush()
	return w.blocks
}

func (w *wikiReader) line(text string) {
	w.closeLists()
	w.para = append(w.para, text)
}

func (w *wikiReader) flush() {
	w.closeLists()
	if len(w.para) == 0 {
		return
	}
	content := []any{}
	for n, line := range w.para {
		if n > 0 {
			content = append(content, map[string]any{"type": "hardBreak"})
		}
		content = append(content, wikiInline(line)...)
	}
	w.blocks = append(w.blocks, map[string]any{"type": "paragraph", "content": content})
	w.para = nil
}

func (w *wikiReader) closeLists() { w.lists = nil }

func (w *wikiReader) heading(line string) {
	w.flush()
	parts := wikiHeading.FindStringSubmatch(line)
	level := int(parts[1][0] - '0')
	if level > maxWikiHeading {
		level = maxWikiHeading
	}
	w.blocks = append(w.blocks, map[string]any{
		"type": "heading", "attrs": map[string]any{"level": level}, "content": wikiInline(parts[2]),
	})
}

func (w *wikiReader) quote(text string) {
	w.flush()
	w.blocks = append(w.blocks, map[string]any{
		"type":    "blockquote",
		"content": []any{map[string]any{"type": "paragraph", "content": wikiInline(text)}},
	})
}

// fence opens or closes a block of code, which is kept exactly as it was.
func (w *wikiReader) fence() {
	if !w.fenced {
		w.flush()
		w.fenced = true
		return
	}
	w.fenced = false
	if len(w.code) > 0 {
		w.blocks = append(w.blocks, map[string]any{
			"type":    "codeBlock",
			"content": []any{map[string]any{"type": "text", "text": strings.Join(w.code, "\n")}},
		})
	}
	w.code = nil
}

// item adds one line of a list, opening the lists its marker is deep in.
func (w *wikiReader) item(marker, content string) {
	if len(w.para) > 0 {
		w.para = nil
	}
	kind := "bulletList"
	if strings.HasSuffix(marker, "#") {
		kind = "orderedList"
	}
	depth := len(marker)
	for len(w.lists) > depth {
		w.lists = w.lists[:len(w.lists)-1]
	}
	for len(w.lists) < depth {
		w.lists = append(w.lists, w.openList(kind))
	}
	list := w.lists[len(w.lists)-1]
	item := map[string]any{"type": "listItem", "content": []any{
		map[string]any{"type": "paragraph", "content": wikiInline(content)},
	}}
	list["content"] = append(list["content"].([]any), item)
}

// openList starts a list at the end of the blocks, or inside the item above it.
func (w *wikiReader) openList(kind string) map[string]any {
	list := map[string]any{"type": kind, "content": []any{}}
	if len(w.lists) == 0 {
		w.blocks = append(w.blocks, list)
		return list
	}
	parent := w.lists[len(w.lists)-1]
	items := parent["content"].([]any)
	if len(items) == 0 {
		items = append(items, map[string]any{"type": "listItem", "content": []any{}})
		parent["content"] = items
	}
	last := items[len(items)-1].(map[string]any)
	last["content"] = append(last["content"].([]any), list)
	return list
}

// cells reads the values out of a table row, header ruling and all.
func cells(line string) []string {
	var out []string
	for _, cell := range strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|") {
		if cell = strings.TrimSpace(strings.Trim(cell, "|")); cell != "" {
			out = append(out, cell)
		}
	}
	return out
}

// wikiMarkup is what this reads inside a line. A strike is deliberately not
// among them: a hyphen in prose is far more common than a struck word.
var wikiMarkup = regexp.MustCompile(`\[~([^\]]+)\]|\[([^\]|]+)\|([^\]]+)\]|\[(https?://[^\]]+)\]|\{\{([^}]+)\}\}|\*([^*\n]+)\*|_([^_\n]+)_`)

// wikiInline turns one line into text nodes, marked where the markup says so.
func wikiInline(line string) []any {
	out := []any{}
	at := 0
	for _, m := range wikiMarkup.FindAllStringSubmatchIndex(line, -1) {
		if m[0] > at {
			out = append(out, textNode(line[at:m[0]]))
		}
		out = append(out, markedNode(line, m)...)
		at = m[1]
	}
	if at < len(line) {
		out = append(out, textNode(line[at:]))
	}
	if len(out) == 0 {
		return []any{textNode(line)}
	}
	return out
}

// markedNode is one piece of markup as the node it becomes.
func markedNode(line string, m []int) []any {
	group := func(n int) string {
		if m[2*n] < 0 {
			return ""
		}
		return line[m[2*n]:m[2*n+1]]
	}
	switch {
	case group(1) != "":
		// A mention here is a person this tracker knows by id, which a file
		// from another one cannot say; the name stays as text.
		return []any{textNode("@" + group(1))}
	case group(2) != "" && SafeHref(group(3)):
		return []any{linkNode(group(2), group(3))}
	case group(4) != "" && SafeHref(group(4)):
		return []any{linkNode(group(4), group(4))}
	case group(5) != "":
		return []any{textNode(group(5), "code")}
	case group(6) != "":
		return []any{textNode(group(6), "bold")}
	case group(7) != "":
		return []any{textNode(group(7), "italic")}
	}
	return []any{textNode(line[m[0]:m[1]])}
}

func textNode(text string, marks ...string) map[string]any {
	node := map[string]any{"type": "text", "text": text}
	if len(marks) > 0 {
		list := []any{}
		for _, mark := range marks {
			list = append(list, map[string]any{"type": mark})
		}
		node["marks"] = list
	}
	return node
}

func linkNode(text, href string) map[string]any {
	return map[string]any{"type": "text", "text": text, "marks": []any{
		map[string]any{"type": "link", "attrs": map[string]any{"href": href}},
	}}
}
