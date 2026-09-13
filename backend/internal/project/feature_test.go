package project

import (
	"errors"
	"testing"
)

func TestNormalizeFeaturesOrdersAndRefuses(t *testing.T) {
	got, err := NormalizeFeatures([]string{" Sprints ", "board", "sprints", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != FeatureBoard || got[1] != FeatureSprints {
		t.Fatalf("want board then sprints once each, got %v", got)
	}
	if _, err := NormalizeFeatures([]string{"gantt"}); !errors.Is(err, ErrBadFeature) {
		t.Fatalf("want ErrBadFeature, got %v", err)
	}
}

func TestDefaultFeaturesKeepTheDeskToDesks(t *testing.T) {
	for _, f := range DefaultFeatures(KindSoftware) {
		if f.DeskOnly() {
			t.Fatalf("a software project got %s", f)
		}
	}
	desk := &Project{Features: DefaultFeatures(KindService)}
	if !desk.Has(FeatureQueues) || !desk.Has(FeatureSprints) {
		t.Fatal("a desk with no narrowing has everything")
	}
	err := &FeatureOffError{Key: "HELP", Feature: FeatureSprints}
	if !errors.Is(err, ErrFeatureOff) || err.Error() != "HELP does not use sprints" {
		t.Fatalf("unexpected error shape: %v", err)
	}
}
