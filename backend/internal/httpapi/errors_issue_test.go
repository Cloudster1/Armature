package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/issue"
)

func TestAStaleIssueEditIsAConflictThatSaysWhatToDo(t *testing.T) {
	got := toAPIError(fmt.Errorf("update: %w", issue.ErrStale))
	if got.Status != http.StatusConflict || got.Code != "stale_edit" {
		t.Fatalf("got %d %s, want 409 stale_edit", got.Status, got.Code)
	}
	const sentence = "Somebody changed this issue since you opened it. Reload it and apply your edit again."
	if got.Message != sentence {
		t.Fatalf("message = %q, want %q", got.Message, sentence)
	}
}

func TestAnIssueEditCarriesTheUpdatedAtItStartedFrom(t *testing.T) {
	var req updateIssueRequest
	if err := json.Unmarshal([]byte(`{"summary":"x","expectedUpdatedAt":"2026-10-10T09:30:00.123456Z"}`), &req); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 10, 9, 30, 0, 123456000, time.UTC)
	if req.ExpectedUpdatedAt == nil || !req.ExpectedUpdatedAt.Equal(want) {
		t.Fatalf("expectedUpdatedAt = %v, want %v", req.ExpectedUpdatedAt, want)
	}
	if in := req.toInput(); in.ExpectedUpdatedAt == nil || !in.ExpectedUpdatedAt.Equal(want) {
		t.Fatalf("the service is not told where the edit started: %+v", in)
	}
}
