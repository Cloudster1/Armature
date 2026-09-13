package desk

import (
	"testing"
	"time"
)

func at(minutes int) *time.Time {
	t := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
	return &t
}

func TestAClockCountsOnlyWhileRunning(t *testing.T) {
	now := *at(100)

	running := Clock{GoalMinutes: 60, RunningSince: at(10)}
	if got := running.Elapsed(now); got != 90*time.Minute {
		t.Errorf("elapsed = %v, want 90m", got)
	}
	if !running.Breached(now) {
		t.Error("90 minutes into a 60 minute goal is not seen as breached")
	}

	// Paused after 20 minutes of running: the bank is what counts.
	paused := Clock{GoalMinutes: 60, ElapsedSeconds: 20 * 60}
	if got := paused.Elapsed(now); got != 20*time.Minute {
		t.Errorf("paused elapsed = %v, want the banked 20m", got)
	}
	if !paused.Paused() || paused.Breached(now) {
		t.Error("a paused clock with time left is neither running nor breached")
	}

	// Resumed: the bank plus the run since.
	resumed := Clock{GoalMinutes: 60, ElapsedSeconds: 20 * 60, RunningSince: at(90)}
	if got := resumed.Remaining(now); got != 30*time.Minute {
		t.Errorf("remaining = %v, want 60 - 20 - 10 = 30m", got)
	}

	// Completed: the clock stopped where it was, whatever now is.
	done := Clock{GoalMinutes: 60, ElapsedSeconds: 45 * 60, RunningSince: at(0), CompletedAt: at(45)}
	if got := done.Elapsed(now); got != 45*time.Minute {
		t.Errorf("completed elapsed = %v, want the 45m it took", got)
	}
	if done.Paused() {
		t.Error("a completed clock is not paused")
	}
}

func TestATimerReadsItself(t *testing.T) {
	now := *at(30)
	timer := Timer{GoalMinutes: 20, elapsedSeconds: 0, RunningSince: at(0)}
	timer.read(now)
	if timer.ElapsedSeconds != 30*60 || timer.RemainingSeconds != -10*60 || !timer.Breached || timer.Paused {
		t.Errorf("reading = %+v", timer)
	}
}
