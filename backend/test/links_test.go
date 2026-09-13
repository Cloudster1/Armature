//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
)

// TestABlockCannotComeBackAround proves the circle guard twice: the service
// refuses it with the way round named, and the database refuses the same row
// written straight through SQL. Symmetric links may still form a triangle.
func TestABlockCannotComeBackAround(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "circling")
	a := h.buildTree(t, ws).story
	b := h.buildTree(t, ws).story
	c := h.buildTree(t, ws).story

	blocks := func(from, to string) (*issue.Link, error) {
		link, _, err := ws.issues.AddLink(ws.ctx, from, issue.LinkInput{TypeName: issue.LinkTypeBlocks, TargetKey: to}, ws.actor)
		return link, err
	}
	if _, err := blocks(a.Key, b.Key); err != nil {
		t.Fatalf("a blocks b: %v", err)
	}
	if _, err := blocks(b.Key, c.Key); err != nil {
		t.Fatalf("b blocks c: %v", err)
	}

	t.Run("the service refuses the link that closes the circle, naming the way round", func(t *testing.T) {
		_, err := blocks(c.Key, a.Key)
		if !errors.Is(err, issue.ErrLinkCycle) {
			t.Fatalf("got %v, want ErrLinkCycle", err)
		}
		want := a.Key + " already blocks " + c.Key + " through " + b.Key + ", so " + c.Key + " cannot block " + a.Key + "."
		if err.Error() != want {
			t.Errorf("sentence:\n got %q\nwant %q", err.Error(), want)
		}
		// The reverse of a direct link is the shortest circle there is.
		_, err = blocks(b.Key, a.Key)
		if !errors.Is(err, issue.ErrLinkCycle) || strings.Contains(err.Error(), "through") {
			t.Errorf("reverse: got %v", err)
		}
	})

	t.Run("the database refuses the same row written through SQL", func(t *testing.T) {
		_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO issue_link (org_id, link_type_id, source_id, target_id)
				SELECT current_org_id(), (SELECT id FROM issue_link_type WHERE lower(name) = 'blocks'), $1, $2`, c.ID, a.ID)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "come back around") {
			t.Errorf("the trigger let the circle through: %v", err)
		}
	})

	t.Run("a shortcut along the chain is not a circle", func(t *testing.T) {
		if _, err := blocks(a.Key, c.Key); err != nil {
			t.Errorf("a blocks c: %v", err)
		}
	})

	t.Run("a triangle of a symmetric kind is allowed", func(t *testing.T) {
		relate := func(from, to string) {
			if _, _, err := ws.issues.AddLink(ws.ctx, from, issue.LinkInput{TypeName: "Relates", TargetKey: to}, ws.actor); err != nil {
				t.Fatalf("%s relates %s: %v", from, to, err)
			}
		}
		relate(a.Key, b.Key)
		relate(b.Key, c.Key)
		relate(c.Key, a.Key)
	})

	t.Run("the plan names each dependency's link, which is how it is undone", func(t *testing.T) {
		blockers, err := ws.issues.Blockers(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		var found *issue.Blocker
		for i := range blockers {
			if blockers[i].BlockerKey == a.Key && blockers[i].BlockedKey == c.Key {
				found = &blockers[i]
			}
		}
		if found == nil || found.LinkID == uuid.Nil {
			t.Fatalf("a blocks c is not in %v", blockers)
		}
		if _, err := ws.issues.RemoveLink(ws.ctx, found.BlockerKey, found.LinkID); err != nil {
			t.Fatalf("remove: %v", err)
		}
		after, err := ws.issues.Blockers(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(blockers)-1 {
			t.Errorf("%d dependencies left, want %d", len(after), len(blockers)-1)
		}
		// With the shortcut gone, closing the circle is refused once more.
		if _, err := blocks(c.Key, a.Key); !errors.Is(err, issue.ErrLinkCycle) {
			t.Errorf("after removal: %v", err)
		}
	})
}
