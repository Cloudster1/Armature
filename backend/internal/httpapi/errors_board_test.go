package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/rank"
)

func TestADropBetweenCardsOutOfOrderIsAConflictThatSaysWhatToDo(t *testing.T) {
	got := toAPIError(asValidationError(fmt.Errorf("%w: %w", board.ErrBoardChanged, rank.ErrOutOfOrder)))
	if got.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", got.Status)
	}
	const sentence = "The board changed while you moved the card. Reload the board and drop the card again."
	if got.Message != sentence {
		t.Fatalf("message = %q, want %q", got.Message, sentence)
	}
}
