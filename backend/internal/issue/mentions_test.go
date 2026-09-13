package issue

import (
	"testing"

	"github.com/google/uuid"
)

func TestParseMentions(t *testing.T) {
	ada := Member{ID: uuid.New(), Name: "Ada Lovelace", Email: "ada@armature.test"}
	adam := Member{ID: uuid.New(), Name: "Adam", Email: "adam@armature.test"}
	members := []Member{adam, ada}

	cases := []struct {
		text string
		want []uuid.UUID
	}{
		{"nothing here", nil},
		{"ping @Ada Lovelace please", []uuid.UUID{ada.ID}},
		{"ping @ada lovelace, please", []uuid.UUID{ada.ID}},
		{"ping @adam", []uuid.UUID{adam.ID}},
		{"ping @adam@armature.test", []uuid.UUID{adam.ID}},
		{"mail ada@armature.test", nil},
		{"@Adam and @Ada Lovelace", []uuid.UUID{ada.ID, adam.ID}},
		{"@Adamant", nil},
	}
	for _, c := range cases {
		got := ParseMentions(c.text, members)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v, want %v", c.text, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: got %v, want %v", c.text, got, c.want)
			}
		}
	}
}
