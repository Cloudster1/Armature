package nql

import (
	"sort"
	"strings"
	"unicode"
)

// Slot says what kind of word the caret is in.
type Slot string

const (
	// SlotField is where a field name goes, at the start or after AND, OR, a
	// bracket or ORDER BY.
	SlotField Slot = "field"
	// SlotOperator follows a field name.
	SlotOperator Slot = "operator"
	// SlotValue follows an operator or opens an IN list.
	SlotValue Slot = "value"
	// SlotKeyword is where AND, OR, ORDER BY, ASC, DESC or EMPTY go.
	SlotKeyword Slot = "keyword"
	// SlotNone is a place nothing can be offered for.
	SlotNone Slot = "none"
)

// Completion says where the caret is in the sentence, what is half typed,
// and the characters a chosen word replaces.
type Completion struct {
	Slot Slot `json:"slot"`
	// Field is the field the operator or value belongs to, when the slot
	// has one.
	Field string `json:"field,omitempty"`
	// Custom says the field is a custom one named in quotes.
	Custom bool `json:"custom,omitempty"`
	// Prefix is what has been typed of the word so far, without quotes.
	Prefix string `json:"prefix"`
	// From and To bound the characters to replace, counted in characters
	// from the start of the text, To exclusive.
	From int `json:"from"`
	To   int `json:"to"`
	// Quoted says the word being typed opened a quote, so the replacement
	// must close it.
	Quoted bool `json:"quoted"`
	// Words are the completions the catalog alone can offer: field names,
	// operators, keywords, enum values and functions. Values that live in
	// tables are the caller's to add.
	Words []string `json:"words"`
}

// Operators lists the operators a field takes, in the spelling a query uses.
func Operators(field string) []string {
	def, err := lookup(Field{Name: field})
	if err != nil {
		def = fieldDef{kind: fkCustom}
	}
	out := []string{}
	for _, op := range operatorsFor(def.kind) {
		switch op {
		case "in":
			out = append(out, "IN", "NOT IN")
		case "empty":
			out = append(out, "IS EMPTY", "IS NOT EMPTY")
		default:
			out = append(out, op)
		}
	}
	return out
}

// Values lists what the catalog knows a field's values to be: enum values
// and the functions its kind accepts. Names that live in tables are not here.
func Values(field string) []string {
	def, err := lookup(Field{Name: field})
	if err != nil {
		return nil
	}
	switch def.kind {
	case fkEnum, fkPriority:
		return append([]string{}, def.enum...)
	case fkUser:
		return []string{"currentUser()"}
	case fkLookup:
		if def.table == "sprint" {
			return []string{"openSprints()"}
		}
	case fkVersion:
		return []string{"unreleasedVersions()", "releasedVersions()"}
	case fkTimestamp, fkDate:
		return []string{"now()", "startOfDay()", "endOfDay()", "startOfWeek()", "endOfWeek()", "startOfMonth()", "endOfMonth()"}
	}
	return nil
}

// Complete reads the text up to the caret and says what could come next.
// The words offered are only ever ones the parser would accept there, so a
// suggestion is never something the query cannot say.
func Complete(text string, at int) Completion {
	runes := []rune(text)
	if at < 0 {
		at = 0
	}
	if at > len(runes) {
		at = len(runes)
	}
	head := string(runes[:at])

	toks, err := lex(head)
	quoted := false
	if err != nil {
		// The one error a half-typed query raises before the caret is an open
		// quote: the word inside it is the one being typed.
		open := strings.LastIndexAny(head, `"'`)
		if open < 0 {
			return Completion{Slot: SlotNone, From: at, To: at, Words: []string{}}
		}
		before := head[:open]
		toks, err = lex(before)
		if err != nil {
			return Completion{Slot: SlotNone, From: at, To: at, Words: []string{}}
		}
		quoted = true
		prefix := head[open+1:]
		c := slotAfter(toks[:len(toks)-1])
		c.Prefix = prefix
		c.From = len([]rune(before))
		c.To = at
		c.Quoted = true
		c.Words = filterPrefix(c.Words, prefix)
		return c
	}
	toks = toks[:len(toks)-1] // drop EOF

	// A word the caret touches is the one being typed; a caret after a
	// space starts a new one.
	var partial *token
	if len(toks) > 0 {
		last := toks[len(toks)-1]
		end := last.pos - 1 + len([]rune(last.text))
		touching := end == at && at > 0 && !unicode.IsSpace(runes[at-1])
		if touching && (last.kind == kWord || last.kind == kOp || last.kind == kNumber) {
			partial = &last
			toks = toks[:len(toks)-1]
		}
	}
	c := slotAfter(toks)
	if partial != nil {
		c.Prefix = partial.text
		c.From = partial.pos - 1
	} else {
		c.From = at
	}
	c.To = at
	c.Quoted = quoted
	c.Words = filterPrefix(c.Words, c.Prefix)
	return c
}

// slotAfter decides the slot from the tokens already complete before it.
func slotAfter(toks []token) Completion {
	fieldNames := Fields()
	keywordsAfterValue := []string{"AND", "OR", "ORDER BY"}
	if len(toks) == 0 {
		return Completion{Slot: SlotField, Words: fieldNames}
	}
	last := toks[len(toks)-1]
	var last2 token
	if len(toks) > 1 {
		last2 = toks[len(toks)-2]
	}
	inOrder := false
	for i := 0; i+1 < len(toks); i++ {
		if toks[i].kind == kWord && strings.EqualFold(toks[i].text, "ORDER") && strings.EqualFold(toks[i+1].text, "BY") {
			inOrder = true
		}
	}
	word := strings.ToUpper(last.text)
	switch {
	case last.kind == kLParen:
		// IN ( opens a list of values; any other bracket opens a condition.
		if last2.kind == kWord && strings.EqualFold(last2.text, "IN") {
			f := fieldBeforeIn(toks[:len(toks)-1])
			return Completion{Slot: SlotValue, Field: f.Name, Custom: f.Custom, Words: Values(f.Name)}
		}
		return Completion{Slot: SlotField, Words: fieldNames}
	case last.kind == kComma:
		if inOrder {
			return Completion{Slot: SlotField, Words: sortableFields()}
		}
		f := fieldBeforeIn(toks)
		return Completion{Slot: SlotValue, Field: f.Name, Custom: f.Custom, Words: Values(f.Name)}
	case last.kind == kOp:
		f := fieldOf(last2)
		return Completion{Slot: SlotValue, Field: f.Name, Custom: f.Custom, Words: Values(f.Name)}
	case last.kind == kRParen, last.kind == kNumber, last.kind == kDate, last.kind == kDuration:
		return Completion{Slot: SlotKeyword, Words: keywordsAfterValue}
	case last.kind == kString:
		// A quoted word is a value after an operator and a custom field otherwise.
		if last2.kind == kOp {
			return Completion{Slot: SlotKeyword, Words: keywordsAfterValue}
		}
		if inOrder {
			return Completion{Slot: SlotKeyword, Words: []string{"ASC", "DESC"}}
		}
		return Completion{Slot: SlotOperator, Field: last.text, Custom: true, Words: Operators("")}
	case last.kind == kWord:
		switch word {
		case "AND", "OR":
			return Completion{Slot: SlotField, Words: fieldNames}
		case "ORDER":
			return Completion{Slot: SlotKeyword, Words: []string{"BY"}}
		case "BY":
			return Completion{Slot: SlotField, Words: sortableFields()}
		case "ASC", "DESC":
			return Completion{Slot: SlotNone, Words: []string{}}
		case "IS":
			return Completion{Slot: SlotKeyword, Words: []string{"EMPTY", "NOT EMPTY"}}
		case "NOT":
			if last2.kind == kWord && strings.EqualFold(last2.text, "IS") {
				return Completion{Slot: SlotKeyword, Words: []string{"EMPTY"}}
			}
			return Completion{Slot: SlotKeyword, Words: []string{"IN"}}
		case "IN":
			return Completion{Slot: SlotNone, Words: []string{"("}}
		case "EMPTY":
			return Completion{Slot: SlotKeyword, Words: keywordsAfterValue}
		}
		if last2.kind == kOp {
			return Completion{Slot: SlotKeyword, Words: keywordsAfterValue}
		}
		if inOrder {
			return Completion{Slot: SlotKeyword, Words: []string{"ASC", "DESC"}}
		}
		return Completion{Slot: SlotOperator, Field: last.text, Words: Operators(last.text)}
	}
	return Completion{Slot: SlotNone, Words: []string{}}
}

func fieldOf(t token) Field {
	switch t.kind {
	case kString:
		return Field{Name: t.text, Custom: true}
	case kWord:
		return Field{Name: t.text}
	}
	return Field{}
}

// fieldBeforeIn walks back over an IN list to the field it belongs to.
func fieldBeforeIn(toks []token) Field {
	for i := len(toks) - 1; i >= 0; i-- {
		if toks[i].kind == kWord && strings.EqualFold(toks[i].text, "IN") {
			j := i - 1
			if j >= 0 && toks[j].kind == kWord && strings.EqualFold(toks[j].text, "NOT") {
				j--
			}
			if j >= 0 {
				return fieldOf(toks[j])
			}
			return Field{}
		}
	}
	return Field{}
}

func sortableFields() []string {
	out := []string{}
	for _, def := range catalog {
		if def.order != "" {
			out = append(out, def.name)
		}
	}
	sort.Strings(out)
	return out
}

// filterPrefix keeps the words that start with what was typed, case blind.
func filterPrefix(words []string, prefix string) []string {
	out := []string{}
	p := strings.ToLower(prefix)
	for _, w := range words {
		if strings.HasPrefix(strings.ToLower(w), p) {
			out = append(out, w)
		}
	}
	return out
}
