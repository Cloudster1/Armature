package report

import "testing"

func TestResolutionBandsCoverEverything(t *testing.T) {
	if resolutionBuckets[len(resolutionBuckets)-1].upTo != 0 {
		t.Fatal("the last band has to be open-ended")
	}
	for i := 1; i < len(resolutionBuckets)-1; i++ {
		if resolutionBuckets[i].upTo <= resolutionBuckets[i-1].upTo {
			t.Errorf("band %d does not widen", i)
		}
	}
}

func TestFlowWindowFallsBackToTheDefault(t *testing.T) {
	if flowWindow(scope{}) != FlowDefaultDays || flowWindow(scope{days: 14}) != 14 {
		t.Error("the window is the asked days, else the default")
	}
}
