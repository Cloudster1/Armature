package template

import "testing"

func TestEveryTemplateIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tpl := range All() {
		if tpl.Key == "" || tpl.Name == "" || tpl.Description == "" {
			t.Errorf("template %+v is missing a key, name or description", tpl)
		}
		if seen[tpl.Key] {
			t.Errorf("two templates share the key %q", tpl.Key)
		}
		seen[tpl.Key] = true
		if !tpl.Kind.Valid() {
			t.Errorf("%s has kind %q, which is not one", tpl.Key, tpl.Kind)
		}
		if !tpl.BoardType.Valid() {
			t.Errorf("%s has board type %q, which is not one", tpl.Key, tpl.BoardType)
		}
		if (tpl.workflow != nil) != (tpl.WorkflowName != "") {
			t.Errorf("%s names a workflow it does not bring, or the other way round", tpl.Key)
		}
	}
}

func TestTheDefaultIsTheFirstOnOffer(t *testing.T) {
	first := All()[0]
	if first.Key != Default {
		t.Errorf("first template is %q, want the default %q", first.Key, Default)
	}
	found, ok := Find("")
	if !ok || found.Key != Default {
		t.Errorf("Find(\"\") = %q, %v; want the default", found.Key, ok)
	}
}

func TestFindRefusesWhatIsNotThere(t *testing.T) {
	if _, ok := Find("waterfall"); ok {
		t.Error("found a template nobody wrote")
	}
}

// A template's workflow must name only statuses every organization starts
// with, or ones the template itself brings, or the template works for nobody.
func TestTemplateWorkflowsUseKnownStatuses(t *testing.T) {
	for _, tpl := range All() {
		if tpl.workflow == nil {
			continue
		}
		known := map[string]bool{"To Do": true, "In Progress": true, "In Review": true, "Done": true}
		for _, st := range tpl.workflow.statuses {
			if !st.category.Valid() {
				t.Errorf("%s brings status %q with category %q, which is not one", tpl.Key, st.name, st.category)
			}
			known[st.name] = true
		}
		initial := 0
		for _, step := range tpl.workflow.steps {
			if !known[step.status] {
				t.Errorf("%s uses status %q, which a new organization does not have", tpl.Key, step.status)
			}
			if step.initial {
				initial++
			}
		}
		if initial != 1 {
			t.Errorf("%s has %d initial statuses, want exactly one", tpl.Key, initial)
		}
		for _, tr := range tpl.workflow.transitions {
			if (tr.from != "" && !known[tr.from]) || !known[tr.to] {
				t.Errorf("%s transition %q touches a status a new organization does not have", tpl.Key, tr.name)
			}
		}
	}
}
