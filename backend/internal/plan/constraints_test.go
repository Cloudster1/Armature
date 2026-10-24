package plan

import (
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/issue"
)

func epic(key string, children ...issue.Node) issue.Node {
	return node(key, issue.LevelEpic, nil, nil, children...)
}

func TestABlockedParentHoldsBackItsChildren(t *testing.T) {
	cases := []struct {
		name  string
		tree  []issue.Node
		links []issue.Blocker
		want  string
	}{
		{
			"an epic blocking an epic",
			[]issue.Node{epic("PR-1", task("PR-2", "2026-11-02", "2026-11-06")), epic("PR-3", task("PR-4", "2026-11-04", "2026-11-04"))},
			blocks("PR-1", "PR-3"),
			"PR-4:2026-11-09..2026-11-09",
		},
		{
			"an issue blocking an epic",
			[]issue.Node{task("PR-1", "2026-11-02", "2026-11-06"), epic("PR-3", task("PR-4", "2026-11-04", "2026-11-04"))},
			blocks("PR-1", "PR-3"),
			"PR-4:2026-11-09..2026-11-09",
		},
		{
			"an epic blocking an issue, held until all of its work is done",
			[]issue.Node{epic("PR-1", task("PR-2", "2026-11-02", "2026-11-03"), task("PR-5", "2026-11-04", "2026-11-06")), task("PR-4", "2026-11-04", "2026-11-04")},
			blocks("PR-1", "PR-4"),
			"PR-4:2026-11-09..2026-11-09",
		},
		{
			"two levels down",
			[]issue.Node{
				epic("PR-1", epic("PR-2", task("PR-3", "2026-11-02", "2026-11-06"))),
				epic("PR-4", epic("PR-5", task("PR-6", "2026-11-04", "2026-11-04"))),
			},
			blocks("PR-1", "PR-4"),
			"PR-6:2026-11-09..2026-11-09",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := moveList(align(t, c.tree, AlignInput{Blockers: c.links})); got != c.want {
				t.Errorf("moves = %s, want %s", got, c.want)
			}
			for _, w := range Check(Moved(FromTree(c.tree), Align(FromTree(c.tree), AlignInput{Blockers: c.links, Today: alignToday}).Moves), c.links) {
				if w.Kind == WarnBlockedTooEarly {
					t.Errorf("after the auto plan the plan still says %+v", w)
				}
			}
		})
	}
}

func TestTheWarningNamesTheParentsLinkOncePerIssue(t *testing.T) {
	tree := []issue.Node{
		epic("PR-1", task("PR-2", "2026-11-02", "2026-11-03"), task("PR-5", "2026-11-02", "2026-11-06")),
		epic("PR-3", task("PR-4", "2026-11-04", "2026-11-04")),
	}
	var tooEarly []Warning
	for _, w := range Check(FromTree(tree), blocks("PR-1", "PR-3")) {
		if w.Kind == WarnBlockedTooEarly {
			tooEarly = append(tooEarly, w)
		}
	}
	if len(tooEarly) != 1 || tooEarly[0].IssueKey != "PR-4" {
		t.Fatalf("warnings = %+v, want one, on PR-4", tooEarly)
	}
	if msg := tooEarly[0].Message; !strings.Contains(msg, "PR-5 does not finish until 6 Nov 2026") || !strings.Contains(msg, "PR-1 blocks PR-3") {
		t.Errorf("message %q, want the epic's work that ends last and the link it comes through", msg)
	}
}

func TestALinkWithinOneLineOfParentsIsNotSpread(t *testing.T) {
	tree := []issue.Node{node("PR-1", issue.LevelEpic, date("2026-11-02"), date("2026-11-06"), task("PR-2", "2026-11-04", "2026-11-05"))}
	if got := moveList(align(t, tree, AlignInput{Blockers: blocks("PR-1", "PR-2")})); got != "PR-2:2026-11-09..2026-11-10" {
		t.Errorf("moves = %s, want only the child after its epic, as the link says", got)
	}
}

// A story under the blocked epic that blocks a story under the blocking one says two things at once.
func TestContradictoryLinksThroughParentsMoveNothingAndEnd(t *testing.T) {
	tree := []issue.Node{
		epic("PR-1", task("PR-2", "2026-11-02", "2026-11-06")),
		epic("PR-3", task("PR-4", "2026-11-04", "2026-11-04")),
	}
	links := blocks("PR-1", "PR-3", "PR-4", "PR-2")
	if moves := align(t, tree, AlignInput{Blockers: links}); len(moves) != 0 {
		t.Errorf("moves = %s, want none: the two links disagree", moveList(moves))
	}
	if warnings := Check(FromTree(tree), links); len(warnings) == 0 {
		t.Error("the disagreement left no warning to read")
	}
}

// A parent between the link and the work, with dates only from its children, is not what the work waits on.
func TestWorkWaitsOnWorkNotOnTheParentsBetween(t *testing.T) {
	tree := []issue.Node{
		epic("PR-1", epic("PR-2", task("PR-3", "2026-11-02", "2026-11-06"))),
		epic("PR-4", epic("PR-5", task("PR-6", "2026-11-04", "2026-11-04"))),
	}
	moves := align(t, tree, AlignInput{Blockers: blocks("PR-1", "PR-4")})
	if len(moves) != 1 || moves[0].After != "PR-3" {
		t.Errorf("moves = %+v, want PR-6 after PR-3", moves)
	}
	for _, c := range Constraints(FromTree(tree), blocks("PR-1", "PR-4")) {
		if c.BlockerKey != "PR-3" || c.BlockedKey != "PR-6" {
			t.Errorf("constraint %s before %s, want only PR-3 before PR-6", c.BlockerKey, c.BlockedKey)
		}
	}
}
