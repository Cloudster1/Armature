package report

import (
	"testing"

	"github.com/armature/armature/backend/internal/project"
)

func TestPercentileIsNearestRank(t *testing.T) {
	values := []float64{5, 1, 4, 2, 3}
	if got := percentile(values, 0.5); got != 3 {
		t.Errorf("median = %v, want 3", got)
	}
	if got := percentile(values, 0.9); got != 5 {
		t.Errorf("p90 = %v, want 5", got)
	}
	if got := percentile(nil, 0.5); got != 0 {
		t.Errorf("median of nothing = %v, want 0", got)
	}
	if got := mean(values); got != 3 {
		t.Errorf("mean = %v, want 3", got)
	}
}

func TestEveryKindIsOfferedSomewhereAndKnown(t *testing.T) {
	seen := map[Kind]bool{}
	for _, kind := range []project.Kind{project.KindSoftware, project.KindService, project.KindBusiness} {
		for _, k := range Kinds(kind) {
			seen[k.Kind] = true
			if k.Width != 1 && k.Width != 2 {
				t.Errorf("%s is %d wide", k.Kind, k.Width)
			}
		}
		overview, ok := findBuiltIn(OverviewKey(kind))
		if !ok {
			t.Fatalf("a %s project has no overview template", kind)
		}
		for _, w := range overview.Widgets {
			info, ok := infoFor(w.Kind)
			if !ok {
				t.Errorf("a %s project starts with %q, which is not a report", kind, w.Kind)
			}
			if !info.suits(kind) {
				t.Errorf("a %s project starts with %s, which is not offered to it", kind, w.Kind)
			}
		}
	}
	// Every built-in template names only kinds that exist, so the chooser
	// never offers a picture the server cannot draw.
	for _, tpl := range builtIn {
		for _, w := range tpl.Widgets {
			if _, ok := infoFor(w.Kind); !ok {
				t.Errorf("template %s names %q, which is not a report", tpl.Key, w.Kind)
			}
		}
	}
	for _, k := range kinds {
		if !seen[k.Kind] {
			t.Errorf("%s is offered to no kind of project", k.Kind)
		}
	}
}

func TestWindowIsBounded(t *testing.T) {
	if got := (Params{}).window(); got != DefaultDays {
		t.Errorf("default window = %d", got)
	}
	if got := (Params{Days: 10_000}).window(); got != MaxDays {
		t.Errorf("huge window = %d, want capped", got)
	}
}
