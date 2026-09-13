// Package nql is the query language: what a person types to find issues,
// parsed and compiled to SQL with every value bound and every identifier ours.
package nql

import "fmt"

// Error says what is wrong with a query and where. Msg is a sentence that
// names what to do; Pos lets the client point at the character.
type Error struct {
	// Pos is the 1-based character the trouble starts at.
	Pos int
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func errAt(pos int, format string, args ...any) *Error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}
