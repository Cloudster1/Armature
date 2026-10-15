package board

import (
	"testing"

	"github.com/armature/armature/backend/internal/nql"
)

// A team or type name with quotes or backslashes in it still reads back as that name.
func TestQuotedNamesReadBackInAQuery(t *testing.T) {
	for _, name := range []string{"Platform", `Night "owls"`, `back\slash`, `trailing\`, "Équipe"} {
		q, err := nql.Parse("team = " + quoteNQL(name))
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		cmp, ok := q.Where.(nql.Compare)
		if !ok || cmp.Value.Text != name {
			t.Errorf("%q reads back as %s", name, q)
		}
	}
}
