package demo

import (
	"encoding/json"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/armature/armature/backend/internal/issue"
)

// maxHeading is the deepest heading a description shows; deeper ones are read as that.
const maxHeading = 3

// githubMarkdown reads an issue body as GitHub writes it: tables, task lists, strikethrough and bare links.
var githubMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// node is one node of the document a description is.
type node struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Marks   []mark         `json:"marks,omitempty"`
	Content []node         `json:"content,omitempty"`
}

type mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Markdown turns a GitHub issue body into a description, keeping only what the tracker shows.
// base resolves a relative link, such as docs/api.md, against the repository it was written in.
func Markdown(body, base string) json.RawMessage {
	source := []byte(body)
	root := githubMarkdown.Parser().Parse(text.NewReader(source))
	c := converter{source: source, base: base}
	doc := node{Type: "doc", Content: c.blocks(root)}
	if len(doc.Content) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(doc)
	return encoded
}

type converter struct {
	source []byte
	base   string
}

// blocks converts the block children of n; a block with no counterpart becomes paragraphs of its text.
func (c converter) blocks(n gast.Node) []node {
	var out []node
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		out = append(out, c.block(child)...)
	}
	return out
}

func (c converter) block(n gast.Node) []node {
	switch b := n.(type) {
	case *gast.Paragraph, *gast.TextBlock:
		return paragraph(c.inlines(b, nil))
	case *gast.Heading:
		inline := c.inlines(b, nil)
		if len(inline) == 0 {
			return nil
		}
		return []node{{Type: "heading", Attrs: map[string]any{"level": min(b.Level, maxHeading)}, Content: inline}}
	case *gast.List:
		kind := "bulletList"
		if b.IsOrdered() {
			kind = "orderedList"
		}
		var items []node
		for item := b.FirstChild(); item != nil; item = item.NextSibling() {
			content := c.blocks(item)
			if len(content) == 0 || content[0].Type != "paragraph" {
				// A list item opens with a paragraph, even an empty one.
				content = append([]node{{Type: "paragraph"}}, content...)
			}
			items = append(items, node{Type: "listItem", Content: content})
		}
		if len(items) == 0 {
			return nil
		}
		return []node{{Type: kind, Content: items}}
	case *gast.Blockquote:
		content := c.blocks(b)
		if len(content) == 0 {
			return nil
		}
		return []node{{Type: "blockquote", Content: content}}
	case *gast.FencedCodeBlock:
		return codeBlock(c.lines(b), string(b.Language(c.source)))
	case *gast.CodeBlock:
		return codeBlock(c.lines(b), "")
	case *gast.HTMLBlock:
		return codeBlock(c.lines(b), "html")
	case *east.Table:
		// A table has no node here, so each row reads as a line with its cells side by side.
		var rows []node
		for row := b.FirstChild(); row != nil; row = row.NextSibling() {
			var cells []string
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, strings.TrimSpace(plain(c.inlines(cell, nil))))
			}
			rows = append(rows, paragraph([]node{{Type: "text", Text: strings.Join(cells, " | ")}})...)
		}
		return rows
	case *gast.ThematicBreak, *gast.LinkReferenceDefinition:
		return nil
	}
	return c.blocks(n)
}

// lines is a code block's text as written, without its last line break.
func (c converter) lines(n gast.Node) string {
	var b strings.Builder
	segments := n.Lines()
	for i := range segments.Len() {
		segment := segments.At(i)
		b.Write(segment.Value(c.source))
	}
	return strings.TrimRight(b.String(), "\n")
}

// inlines converts the inline children of n, each carrying the marks of the nodes around it.
func (c converter) inlines(n gast.Node, marks []mark) []node {
	var out []node
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		out = append(out, c.inline(child, marks)...)
	}
	return merge(out)
}

func (c converter) inline(n gast.Node, marks []mark) []node {
	switch i := n.(type) {
	case *gast.Text:
		value := string(util.ResolveNumericReferences(util.ResolveEntityNames(util.UnescapePunctuations(i.Value(c.source)))))
		out := textNode(value, marks)
		switch {
		case i.HardLineBreak():
			out = append(out, node{Type: "hardBreak"})
		case i.SoftLineBreak():
			out = append(out, textNode(" ", marks)...)
		}
		return out
	case *gast.String:
		return textNode(string(i.Value), marks)
	case *gast.CodeSpan:
		return textNode(plain(c.inlines(i, nil)), with(marks, mark{Type: "code"}))
	case *gast.Emphasis:
		kind := "italic"
		if i.Level >= 2 {
			kind = "bold"
		}
		return c.inlines(i, with(marks, mark{Type: kind}))
	case *east.Strikethrough:
		return c.inlines(i, with(marks, mark{Type: "strike"}))
	case *gast.Link:
		return c.linked(c.inlines(i, nil), string(i.Destination), marks)
	case *gast.Image:
		label := plain(c.inlines(i, nil))
		if label == "" {
			label = string(i.Destination)
		}
		return c.linked(textNode(label, nil), string(i.Destination), marks)
	case *gast.AutoLink:
		url := string(i.URL(c.source))
		return c.linked(textNode(string(i.Label(c.source)), nil), url, marks)
	case *east.TaskCheckBox:
		box := "[ ] "
		if i.IsChecked {
			box = "[x] "
		}
		return textNode(box, marks)
	case *gast.RawHTML:
		var b strings.Builder
		for j := range i.Segments.Len() {
			segment := i.Segments.At(j)
			b.Write(segment.Value(c.source))
		}
		return textNode(b.String(), marks)
	}
	return c.inlines(n, marks)
}

// linked puts a link on the nodes when the address is one the tracker opens.
func (c converter) linked(content []node, href string, marks []mark) []node {
	href = c.resolve(href)
	for i := range content {
		content[i].Marks = append(append([]mark{}, marks...), content[i].Marks...)
		if href != "" && content[i].Type == "text" {
			content[i].Marks = with(content[i].Marks, mark{Type: "link", Attrs: map[string]any{"href": href}})
		}
	}
	return content
}

// resolve reads a relative address against the repository; an anchor or anything that runs is dropped.
func (c converter) resolve(href string) string {
	href = strings.TrimSpace(href)
	switch {
	case href == "" || strings.HasPrefix(href, "#"):
		return ""
	case issue.SafeHref(href):
		return href
	case strings.Contains(href, ":") || c.base == "":
		return ""
	}
	return strings.TrimSuffix(c.base, "/") + "/" + strings.TrimPrefix(href, "/")
}

func textNode(value string, marks []mark) []node {
	if value == "" {
		return nil
	}
	return []node{{Type: "text", Text: value, Marks: marks}}
}

func codeBlock(code, language string) []node {
	out := node{Type: "codeBlock"}
	if language != "" {
		out.Attrs = map[string]any{"language": language}
	}
	if code != "" {
		out.Content = []node{{Type: "text", Text: code}}
	}
	return []node{out}
}

func paragraph(content []node) []node {
	content = trim(content)
	if len(content) == 0 {
		return nil
	}
	return []node{{Type: "paragraph", Content: content}}
}

// trim takes the spaces a line break left at either end of a paragraph.
func trim(content []node) []node {
	for len(content) > 0 && content[len(content)-1].Type == "text" {
		last := &content[len(content)-1]
		last.Text = strings.TrimRight(last.Text, " ")
		if last.Text != "" {
			break
		}
		content = content[:len(content)-1]
	}
	for len(content) > 0 && content[0].Type == "hardBreak" {
		content = content[1:]
	}
	for len(content) > 0 && content[len(content)-1].Type == "hardBreak" {
		content = content[:len(content)-1]
	}
	return content
}

// merge joins neighbouring text with the same marks, which is how the editor writes it.
func merge(content []node) []node {
	var out []node
	for _, n := range content {
		if len(out) > 0 {
			last := &out[len(out)-1]
			if n.Type == "text" && last.Type == "text" && sameMarks(last.Marks, n.Marks) {
				last.Text += n.Text
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func sameMarks(a, b []mark) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// with adds a mark once, leaving the caller's slice as it was.
func with(marks []mark, m mark) []mark {
	for _, have := range marks {
		if have.Type == m.Type {
			return marks
		}
	}
	return append(append([]mark{}, marks...), m)
}

// plain is the text of inline nodes, a line break read as a space.
func plain(content []node) string {
	var b strings.Builder
	for _, n := range content {
		if n.Type == "hardBreak" {
			b.WriteString(" ")
		}
		b.WriteString(n.Text)
	}
	return b.String()
}
