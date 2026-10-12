package httpapi

import (
	"testing"
	"time"
)

// A development stack shortens the window so test runs in a row are let in;
// an address refused inside the window is let in again once it has passed.
func TestTheDoorWindowIsHowLongAnAddressIsRemembered(t *testing.T) {
	t.Cleanup(func() { SetDoorWindow(DoorWindow); ResetThrottles() })
	ResetThrottles()
	const window = time.Minute
	SetDoorWindow(window)

	start := time.Now()
	for i := 0; i < CodeRequestsPerWindow; i++ {
		if !codeRequests.allow("10.0.0.1", start) {
			t.Fatalf("request %d was refused inside the limit", i+1)
		}
	}
	if codeRequests.allow("10.0.0.1", start.Add(window-time.Second)) {
		t.Fatal("a request past the limit inside the window was let in")
	}
	if !codeRequests.allow("10.0.0.1", start.Add(window)) {
		t.Fatal("a request once the window had passed was refused")
	}
}

// Zero would remember nobody at all, so it leaves the window as it was.
func TestANonPositiveDoorWindowChangesNothing(t *testing.T) {
	t.Cleanup(func() { SetDoorWindow(DoorWindow) })
	SetDoorWindow(time.Minute)
	SetDoorWindow(0)
	for _, b := range []*throttle{codeRequests, openEntries, codeRedemptions, credentialTries, resetRequests, resetAddresses} {
		if b.window != time.Minute {
			t.Errorf("a zero window replaced %v", b.window)
		}
	}
}
