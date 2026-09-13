//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bulk"
	"github.com/armature/armature/backend/internal/csvio"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/filter"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/project"
)

// A saved filter is a query with a name: the owner's, shared when they say
// so, starred by anyone who may see it, and mailed on a schedule.
func TestASavedFilterIsAQueryWithAName(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "filters")
	bob := h.joinExisting(t, ws, h.email(t, "bobfilter"), "member")
	filters := filter.NewService(h.cluster)
	me := ws.actor.UserID

	if _, _, err := filters.Create(ws.ctx, me, filter.Input{Name: ptr("Broken"), Query: ptr("status = = x")}); err == nil {
		t.Error("a query the search would refuse was saved")
	}
	mine, _, err := filters.Create(ws.ctx, me, filter.Input{Name: ptr("My open work"), Query: ptr("assignee = currentUser() AND statusCategory != done")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := filters.Create(ws.ctx, me, filter.Input{Name: ptr("my open work"), Query: ptr("project = X")}); err == nil {
		t.Error("a second filter with the same name was accepted")
	}

	// Bob sees nothing of it until it is shared, and cannot change it then.
	if seen, _ := filters.List(ws.ctx, bob); len(seen) != 0 {
		t.Errorf("bob sees %d filters before any is shared", len(seen))
	}
	if _, _, err := filters.Update(ws.ctx, mine.ID, me, filter.Input{Shared: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	seen, _ := filters.List(ws.ctx, bob)
	if len(seen) != 1 || seen[0].OwnerID != me || seen[0].Starred {
		t.Fatalf("bob sees %+v, want the shared filter, unstarred", seen)
	}
	if _, _, err := filters.Update(ws.ctx, mine.ID, bob, filter.Input{Name: ptr("Bob's now")}); err == nil {
		t.Error("bob changed a filter that is not his")
	}
	starred, _, err := filters.Star(ws.ctx, mine.ID, bob, true)
	if err != nil || !starred.Starred {
		t.Fatalf("star: %+v %v", starred, err)
	}

	// A customer cannot share, by the service and by the database.
	customer := ws.customerOf(t, h, "custfilter")
	if _, _, err := filters.Create(ws.ctx, customer.UserID, filter.Input{Name: ptr("Theirs"), Query: ptr("project = X"), Shared: ptr(true)}); err == nil {
		t.Error("a customer shared a filter through the service")
	}
	if _, err := h.super.Exec(context.Background(), `INSERT INTO saved_filter (org_id, owner_id, name, query, shared) VALUES ($1, $2, 'x', 'project = X', true)`, ws.orgID, customer.UserID); err == nil || !strings.Contains(err.Error(), "customer cannot share") {
		t.Errorf("the database let a customer share: %v", err)
	}

	// A subscription mails the result on its schedule, as the subscriber.
	made := ws.newIssue(t, "Mine to do")
	mePtr := &me
	if _, _, err := ws.issues.Update(ws.ctx, made.Key, issue.UpdateInput{Assignee: &mePtr}, ws.actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := filters.Subscribe(ws.ctx, mine.ID, me, filter.Subscription{Schedule: filter.Daily, Hour: 7}); err != nil {
		t.Fatal(err)
	}
	mailer := &fakeMailer{}
	subs := filter.NewSubscriptions(h.cluster, ws.issues, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	sent, err := subs.Once(context.Background())
	if err != nil || sent < 1 {
		t.Fatalf("subscriptions sent %d, %v", sent, err)
	}
	var mail *struct{ To, Subject, Body string }
	for i := range mailer.sent {
		if strings.Contains(mailer.sent[i].Subject, "My open work") {
			mail = &mailer.sent[i]
		}
	}
	if mail == nil || !strings.Contains(mail.Body, made.Key) || !strings.Contains(mail.Body, "http://app.test/filters/"+mine.ID.String()) {
		t.Fatalf("subscription mail = %+v, want the issue and the link", mail)
	}
	if again, _ := subs.Once(context.Background()); again != 0 {
		t.Error("the subscription went out twice in one day")
	}
	if _, _, err := filters.Unsubscribe(ws.ctx, mine.ID, me); err != nil {
		t.Fatal(err)
	}
	if _, err := filters.Delete(ws.ctx, mine.ID, bob); err == nil {
		t.Error("bob deleted a filter that is not his")
	}
	if _, err := filters.Delete(ws.ctx, mine.ID, me); err != nil {
		t.Fatal(err)
	}
}

// Many issues at once: one refused leaves the rest applied and says why.
// A file in makes an issue per row after a dry run; a query out is a file.
func TestBulkEditAndCSV(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "bulks")
	labels := label.NewService(h.cluster)
	edits := bulk.NewService(ws.issues, labels)

	a := ws.newIssue(t, "First")
	b := ws.newIssue(t, "Second")
	c := ws.newIssue(t, "Third, already going")
	ws.move(t, h, c.Key, "Start progress")

	high := issue.PriorityHigh
	result, _, err := edits.Edit(ws.ctx, []string{a.Key, b.Key, c.Key}, bulk.Input{Priority: &high, AddLabels: []string{"batch"}, Transition: "Start progress"}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Applied) != 2 || len(result.Refused) != 1 || result.Refused[0].Key != c.Key || !strings.Contains(result.Refused[0].Reason, "not offered") {
		t.Fatalf("bulk = %+v, want two applied and the third refused for its transition", result)
	}
	after, _ := ws.issues.ByKey(ws.ctx, c.Key)
	if after.Priority != high || len(after.Labels) != 1 {
		t.Errorf("the refused issue still took what came before the refusal: %+v", after)
	}
	if _, _, err := edits.Edit(ws.ctx, []string{a.Key}, bulk.Input{}, ws.actor); err == nil {
		t.Error("an empty change was accepted")
	}

	// Export carries the chosen columns.
	files := csvio.NewService(h.cluster, ws.issues, labels, field.NewService(h.cluster), h.authService())
	var out bytes.Buffer
	if err := files.Export(ws.ctx, &out, issue.Filter{ProjectKey: ws.project.Key}, []string{"key", "summary", "labels", "priority"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || lines[0] != "key,summary,labels,priority" || !strings.Contains(lines[3], `"Third, already going",batch,high`) {
		t.Fatalf("export = %q", out.String())
	}
	if err := files.Export(ws.ctx, &out, issue.Filter{}, []string{"colour"}); err == nil {
		t.Error("an unknown column was accepted")
	}

	// Import: a dry run makes nothing; the real run makes a row an issue, and
	// says which rows it could not.
	csvText := "Title,Priority,Due,Tags,Who\nImported one,high,2026-10-01,fresh; batch,\nImported two,urgent,,,\nImported three,low,2026-10-02,,nobody@nowhere.test\n"
	mapping := csvio.Guess(strings.Split(strings.Split(csvText, "\n")[0], ","))
	mapping["assignee"] = []int{4}
	before, _ := ws.issues.List(ws.ctx, issue.Filter{ProjectKey: ws.project.Key}, issue.Page{Limit: 50})
	dry, _, err := files.Import(ws.ctx, ws.project.Key, []byte(csvText), csvio.ImportRequest{Mapping: mapping, DryRun: true}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	// The unknown priority refuses its row; the unknown person only costs the
	// issue its assignee, and the report says so.
	if !dry.DryRun || len(dry.Imported) != 2 || len(dry.Refused) != 1 {
		t.Fatalf("dry run = %+v, want two rows that would be made and one refused", dry)
	}
	if len(dry.Notes) == 0 {
		t.Error("nothing was said about the name nobody here answers to")
	}
	afterDry, _ := ws.issues.List(ws.ctx, issue.Filter{ProjectKey: ws.project.Key}, issue.Page{Limit: 50})
	if afterDry.Total != before.Total {
		t.Fatal("a dry run made issues")
	}
	real, _, err := files.Import(ws.ctx, ws.project.Key, []byte(csvText), csvio.ImportRequest{Mapping: mapping}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(real.Imported) != 2 || real.JobID == nil {
		t.Fatalf("import = %+v", real)
	}
	imported, _ := ws.issues.ByKey(ws.ctx, real.Imported[0])
	if imported.Priority != high || imported.DueDate == nil || len(imported.Labels) != 2 {
		t.Errorf("imported = %+v, want priority, due and two labels", imported)
	}
	if _, _, err := files.Import(ws.ctx, ws.project.Key, []byte(csvText), csvio.ImportRequest{Mapping: csvio.Mapping{"priority": {1}}}, ws.actor); err == nil {
		t.Error("an import with no summary column was accepted")
	}
}

// A copy takes the fields, labels and versions and starts its own history; a
// move keeps the history, gets the target's next key, keeps the old address
// working, and clears what belonged to the old project.
func TestCloneAndMove(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "movers")
	labels := label.NewService(h.cluster)
	source := ws.newIssue(t, "Original")
	if _, _, err := labels.SetIssueLabels(ws.ctx, source.Key, []string{"keep"}, ws.actor); err != nil {
		t.Fatal(err)
	}
	other := ws.newIssue(t, "Blocked by the original")
	if _, _, err := ws.issues.AddLink(ws.ctx, source.Key, issue.LinkInput{TypeName: "blocks", TargetKey: other.Key}, ws.actor); err != nil {
		t.Fatal(err)
	}
	ws.move(t, h, source.Key, "Start progress")

	copied, _, err := ws.issues.Clone(ws.ctx, source.Key, issue.CloneOptions{Links: true}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Key == source.Key || copied.Summary != "Original" || len(copied.Labels) != 1 || copied.Status.Name != "To Do" {
		t.Fatalf("copy = %+v, want the fields and labels, in the first status", copied)
	}
	links, _ := ws.issues.Links(ws.ctx, copied.Key)
	if len(links) != 1 || links[0].Issue.Key != other.Key {
		t.Errorf("copy links = %+v, want the block carried over", links)
	}
	history, _ := ws.issues.History(ws.ctx, copied.Key)
	if len(history) != 2 || history[0].Changes[0].Field != "clonedFrom" || history[0].Changes[0].To != source.Key {
		t.Errorf("copy history = %+v, want created and cloned from", history)
	}

	// Move to a project whose workflow does not have In Progress: refused
	// until a status is chosen; then the history stays and the old key finds it.
	target, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "Target", Key: "TGT" + strings.ToUpper(uuid.New().String()[:3])}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	moved, _, err := ws.issues.Move(ws.ctx, source.Key, issue.MoveInput{ProjectKey: target.Key}, ws.actor)
	if err != nil {
		t.Fatalf("move to a project with the same workflow: %v", err)
	}
	if moved.ProjectKey != target.Key || moved.ID != source.ID || !strings.HasPrefix(moved.Key, target.Key+"-") || moved.Status.Name != "In Progress" {
		t.Fatalf("moved = %+v", moved)
	}
	byOld, err := ws.issues.ByKey(ws.ctx, source.Key)
	if err != nil || byOld.ID != source.ID {
		t.Errorf("the old key no longer finds the issue: %v", err)
	}
	history, _ = ws.issues.History(ws.ctx, moved.Key)
	if len(history) < 3 || history[0].Changes[0].Field != "project" || history[0].Changes[1].Field != "key" {
		t.Errorf("moved history = %+v, want the project and key changes on top of the old history", history)
	}
	comments, _ := ws.issues.Comments(ws.ctx, moved.Key, true)
	if len(comments) != 1 || !strings.Contains(string(comments[0].Body), "Moved from "+source.Key) {
		t.Errorf("comments = %d, want the note of the move", len(comments))
	}
	if _, _, err := ws.issues.Move(ws.ctx, moved.Key, issue.MoveInput{ProjectKey: target.Key}, ws.actor); err == nil {
		t.Error("moving to the same project was accepted")
	}
}

// Every operation over the API, the way the pages use them.
func TestFiltersBulkAndFilesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "filtersapi")
	project := want(t, c.post("/api/v1/projects", map[string]any{"name": "Files", "key": "FIL" + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban"}), http.StatusCreated, "project")
	key := obj(t, project, "project")["key"].(string)
	target := want(t, c.post("/api/v1/projects", map[string]any{"name": "Elsewhere", "key": "ELS" + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban"}), http.StatusCreated, "target project")
	targetKey := obj(t, target, "project")["key"].(string)

	want(t, c.post("/api/v1/filters", map[string]any{"name": "Bad", "query": "status = = x"}), http.StatusBadRequest, "a bad query")
	made := want(t, c.post("/api/v1/filters", map[string]any{"name": "Open work", "query": "statusCategory != done", "shared": true}), http.StatusCreated, "a filter")
	filterID := idOf(t, made, "filter")
	want(t, c.post("/api/v1/filters", map[string]any{"name": "Open work", "query": "project = X"}), http.StatusConflict, "a duplicate name")
	want(t, c.get("/api/v1/filters"), http.StatusOK, "filters")
	want(t, c.get("/api/v1/filters/"+filterID), http.StatusOK, "one filter")
	want(t, c.get("/api/v1/filters/"+uuid.New().String()), http.StatusNotFound, "a filter that is not here")
	want(t, c.patch("/api/v1/filters/"+filterID, map[string]any{"columns": []string{"key", "summary"}}), http.StatusOK, "editing")
	want(t, c.put("/api/v1/filters/"+filterID+"/star", nil), http.StatusOK, "starring")
	want(t, c.delete("/api/v1/filters/"+filterID+"/star"), http.StatusOK, "unstarring")
	want(t, c.put("/api/v1/filters/"+filterID+"/subscription", map[string]any{"schedule": "weekly", "hour": 9}), http.StatusUnprocessableEntity, "weekly without a day")
	want(t, c.put("/api/v1/filters/"+filterID+"/subscription", map[string]any{"schedule": "daily", "hour": 9}), http.StatusOK, "subscribing")
	want(t, c.delete("/api/v1/filters/"+filterID+"/subscription"), http.StatusOK, "unsubscribing")
	want(t, c.delete("/api/v1/filters/"+uuid.New().String()+"/subscription"), http.StatusNotFound, "unsubscribing from nothing")

	var keys []string
	for _, summary := range []string{"One", "Two", "Three"} {
		r := want(t, c.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": summary}), http.StatusCreated, "issue")
		keys = append(keys, obj(t, r, "issue")["key"].(string))
	}
	ran := want(t, c.get("/api/v1/filters/"+filterID+"/issues?limit=10"), http.StatusOK, "running the filter")
	if ran.Body["total"].(float64) < 3 {
		t.Errorf("filter total = %v", ran.Body["total"])
	}

	bulked := want(t, c.post("/api/v1/issues/bulk", map[string]any{"keys": keys, "change": map[string]any{"priority": "high", "addLabels": []string{"batch"}}}), http.StatusOK, "bulk")
	if len(list(t, bulked, "applied")) != 3 {
		t.Errorf("bulk = %s", bulked.Raw)
	}
	want(t, c.post("/api/v1/issues/bulk", map[string]any{"keys": keys, "change": map[string]any{}}), http.StatusUnprocessableEntity, "an empty change")
	want(t, c.post("/api/v1/issues/bulk", map[string]any{"keys": []string{"not a key"}, "change": map[string]any{"priority": "high"}}), http.StatusBadRequest, "a bad key")

	export, body := c.download("/api/v1/issues/export?project=" + key + "&columns=key,summary,labels")
	if export.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "key,summary,labels\n") || !strings.Contains(string(body), "batch") {
		t.Errorf("export = %d %q", export.StatusCode, string(body))
	}
	if r, _ := c.download("/api/v1/issues/export?columns=colour"); r.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("an unknown export column answered %d", r.StatusCode)
	}

	csvText := "Title,Priority\nFrom a file,high\nAnother,low\n"
	preview := c.uploadWith("/api/v1/projects/"+key+"/import/preview", []byte(csvText), "")
	if preview.Status != http.StatusOK || obj(t, preview, "mapping")["summary"] == nil {
		t.Fatalf("preview = %d %s", preview.Status, preview.Raw)
	}
	if bad := c.uploadWith("/api/v1/projects/"+key+"/import/preview", []byte(""), ""); bad.Status != http.StatusUnprocessableEntity {
		t.Errorf("an empty file previewed with %d", bad.Status)
	}
	dry := c.uploadWith("/api/v1/projects/"+key+"/import?dryRun=true", []byte(csvText), `{"summary":[0],"priority":[1]}`)
	if dry.Status != http.StatusOK || len(list(t, dry, "report", "imported")) != 2 {
		t.Fatalf("dry run = %d %s", dry.Status, dry.Raw)
	}
	real := c.uploadWith("/api/v1/projects/"+key+"/import", []byte(csvText), `{"summary":[0],"priority":[1]}`)
	if real.Status != http.StatusOK || len(list(t, real, "report", "imported")) != 2 {
		t.Fatalf("import = %d %s", real.Status, real.Raw)
	}
	// A mapping that names no summary is refused; no mapping at all is guessed.
	if bad := c.uploadWith("/api/v1/projects/"+key+"/import", []byte(csvText), `{"priority":[1]}`); bad.Status != http.StatusUnprocessableEntity {
		t.Errorf("an import without a summary column answered %d", bad.Status)
	}

	cloned := want(t, c.post("/api/v1/issues/"+keys[0]+"/clone", map[string]any{"links": false, "subtasks": false, "summary": "A copy"}), http.StatusCreated, "clone")
	if obj(t, cloned, "issue")["summary"] != "A copy" {
		t.Errorf("clone = %s", cloned.Raw)
	}
	want(t, c.post("/api/v1/issues/"+key+"-999/clone", map[string]any{}), http.StatusNotFound, "cloning nothing")
	want(t, c.post("/api/v1/issues/"+keys[1]+"/move", map[string]any{}), http.StatusBadRequest, "moving nowhere")
	moved := want(t, c.post("/api/v1/issues/"+keys[1]+"/move", map[string]any{"projectKey": targetKey}), http.StatusOK, "move")
	newKey := obj(t, moved, "issue")["key"].(string)
	if !strings.HasPrefix(newKey, targetKey+"-") {
		t.Errorf("moved key = %s", newKey)
	}
	if r := want(t, c.get("/api/v1/issues/"+keys[1]), http.StatusOK, "the old key"); obj(t, r, "issue")["key"] != newKey {
		t.Errorf("the old key answered %s", r.Raw)
	}
	want(t, c.delete("/api/v1/filters/"+filterID), http.StatusNoContent, "deleting")
}

// uploadWith posts a CSV file with an optional mapping part.
func (c *client) uploadWith(path string, data []byte, mapping string) response {
	c.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "issues.csv")
	if err != nil {
		c.t.Fatal(err)
	}
	_, _ = part.Write(data)
	if mapping != "" {
		_ = form.WriteField("mapping", mapping)
	}
	_ = form.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.base+path, &body)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("upload %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := response{Status: resp.StatusCode, Header: resp.Header, Raw: string(raw)}
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &out.Body)
	}
	apiContract.observe(c.t, http.MethodPost, path, resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	return out
}
