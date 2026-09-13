package project

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Feature is one page a project may have, with the endpoints behind it. The
// value is the page's route slug, so the sidebar and the guard agree by name.
type Feature string

const (
	FeatureBoard        Feature = "board"
	FeatureSprints      Feature = "sprints"
	FeaturePlan         Feature = "plan"
	FeatureCalendar     Feature = "calendar"
	FeatureMilestones   Feature = "milestones"
	FeatureReleases     Feature = "releases"
	FeatureComponents   Feature = "components"
	FeatureHierarchy    Feature = "hierarchy"
	FeatureDashboard    Feature = "dashboard"
	FeatureQueues       Feature = "queues"
	FeatureDesk         Feature = "desk"
	FeatureTeams        Feature = "teams"
	FeatureRepositories Feature = "repositories"
	FeatureAutomation   Feature = "automation"
	FeatureImport       Feature = "import"
)

// allFeatures is every feature in the order pages are listed.
var allFeatures = []Feature{
	FeatureBoard, FeatureSprints, FeaturePlan, FeatureCalendar, FeatureMilestones,
	FeatureReleases, FeatureComponents, FeatureHierarchy, FeatureDashboard,
	FeatureQueues, FeatureDesk, FeatureTeams, FeatureRepositories,
	FeatureAutomation, FeatureImport,
}

// deskFeatures are the pages only a service desk has.
var deskFeatures = []Feature{FeatureQueues, FeatureDesk}

// AllFeatures returns every feature in display order.
func AllFeatures() []Feature { return slices.Clone(allFeatures) }

// Valid reports whether the name is a feature.
func (f Feature) Valid() bool { return slices.Contains(allFeatures, f) }

// Word is the feature as a sentence names it: "sprints", "the service desk".
func (f Feature) Word() string {
	switch f {
	case FeatureDesk:
		return "the service desk"
	case FeatureBoard:
		return "the board"
	case FeaturePlan:
		return "the plan"
	case FeatureCalendar:
		return "the calendar"
	case FeatureHierarchy:
		return "the hierarchy"
	case FeatureDashboard:
		return "dashboards"
	case FeatureImport:
		return "imports"
	}
	return string(f)
}

// DeskOnly reports whether the feature belongs to a service desk alone.
func (f Feature) DeskOnly() bool { return slices.Contains(deskFeatures, f) }

var (
	// ErrBadFeature is returned for a name that is not a feature.
	ErrBadFeature = errors.New("not a feature")
	// ErrFeatureOff is returned when a project is asked for a page it does not have.
	ErrFeatureOff = errors.New("this project does not use that feature")
)

// FeatureOffError says which project lacks which feature, for a sentence
// that names what to do about it.
type FeatureOffError struct {
	Key     string
	Feature Feature
}

func (e *FeatureOffError) Error() string {
	return fmt.Sprintf("%s does not use %s", e.Key, e.Feature.Word())
}

func (e *FeatureOffError) Is(target error) bool { return target == ErrFeatureOff }

// Has reports whether the project has the feature.
func (p *Project) Has(f Feature) bool { return slices.Contains(p.Features, f) }

// DefaultFeatures is what a project of the kind has when nobody narrowed it:
// everything the kind allows.
func DefaultFeatures(kind Kind) []Feature {
	out := make([]Feature, 0, len(allFeatures))
	for _, f := range allFeatures {
		if f.DeskOnly() && kind != KindService {
			continue
		}
		out = append(out, f)
	}
	return out
}

// NormalizeFeatures reads a list somebody typed or sent: trimmed, deduplicated
// and in display order, refusing a name that is not a feature.
func NormalizeFeatures(raw []string) ([]Feature, error) {
	seen := map[Feature]bool{}
	for _, r := range raw {
		f := Feature(strings.ToLower(strings.TrimSpace(r)))
		if f == "" {
			continue
		}
		if !f.Valid() {
			return nil, fmt.Errorf("%w: %q", ErrBadFeature, r)
		}
		seen[f] = true
	}
	out := make([]Feature, 0, len(seen))
	for _, f := range allFeatures {
		if seen[f] {
			out = append(out, f)
		}
	}
	return out, nil
}

// featureNames is the list as the database column holds it.
func featureNames(features []Feature) []string {
	out := make([]string, len(features))
	for i, f := range features {
		out[i] = string(f)
	}
	return out
}

// parseFeatures reads the column back; a name the code no longer knows is
// dropped rather than shown.
func parseFeatures(names []string) []Feature {
	out := make([]Feature, 0, len(names))
	for _, f := range allFeatures {
		if slices.Contains(names, string(f)) {
			out = append(out, f)
		}
	}
	return out
}
