//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/privacy"
)

func named(name string) *string { return &name }

// aCompany makes a company at a desk, with the customers given in it.
func aCompany(t *testing.T, ws *workspace, d *desk.Service, deskKey, name string, members ...issue.Actor) *desk.Company {
	t.Helper()
	c, _, err := d.CreateCompany(ws.ctx, deskKey, desk.CompanyInput{Name: named(name)})
	if err != nil {
		t.Fatalf("make %s: %v", name, err)
	}
	for _, m := range members {
		if c, _, err = d.AddCompanyMember(ws.ctx, c.ID, m.UserID); err != nil {
			t.Fatalf("put a customer in %s: %v", name, err)
		}
	}
	return c
}

func raiseAt(t *testing.T, ws *workspace, d *desk.Service, deskKey, summary string, share bool, customer issue.Actor) *issue.Issue {
	t.Helper()
	types, err := d.RequestTypes(ws.ctx, deskKey)
	if err != nil || len(types) == 0 {
		t.Fatalf("request types of %s: %v %v", deskKey, types, err)
	}
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: types[0].ID, Summary: summary, ShareWithCompany: share}, customer)
	if err != nil {
		t.Fatalf("raise %q: %v", summary, err)
	}
	return raised
}

func requestKeys(issues []issue.Issue) []string {
	out := []string{}
	for _, i := range issues {
		out = append(out, i.Key)
	}
	return out
}

// A request the reporter shares is read by their colleagues and by nobody
// else, never with its notes, and only while they are colleagues.
func TestACompanyReadsWhatAColleagueShared(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "companies")
	d, p := ws.aDesk(t, h, "Support")
	_, elsewhere := ws.aDesk(t, h, "Elsewhere")
	reporter := ws.customerOf(t, h, "reporter")
	colleague := ws.customerOf(t, h, "colleague")
	rival := ws.customerOf(t, h, "rival")
	stranger := ws.customerOf(t, h, "stranger")
	globex := aCompany(t, ws, d, p.Key, "Globex", reporter, colleague)
	initech := aCompany(t, ws, d, p.Key, "Initech", rival)

	shared := raiseAt(t, ws, d, p.Key, "The VPN is down", true, reporter)
	private := raiseAt(t, ws, d, p.Key, "My own laptop", false, reporter)
	theirs := raiseAt(t, ws, d, p.Key, "Initech's printer", true, rival)
	if shared.Company == nil || shared.Company.ID != globex.ID || !shared.SharedWithCompany {
		t.Fatalf("the shared request came in as %+v shared=%v, want Globex's and shared", shared.Company, shared.SharedWithCompany)
	}
	if private.Company == nil || private.Company.ID != globex.ID || private.SharedWithCompany {
		t.Fatalf("the private request came in as %+v shared=%v, want Globex's and not shared", private.Company, private.SharedWithCompany)
	}

	t.Run("a colleague lists and reads the shared request and nothing else", func(t *testing.T) {
		listed, err := d.SharedRequests(ws.ctx, colleague.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if got := requestKeys(listed); len(got) != 1 || got[0] != shared.Key {
			t.Errorf("shared with the colleague = %v, want only %s", got, shared.Key)
		}
		if _, err := d.Request(ws.ctx, shared.Key, colleague.UserID); err != nil {
			t.Errorf("the colleague reading the shared request: %v", err)
		}
		if _, err := d.Request(ws.ctx, private.Key, colleague.UserID); !errors.Is(err, desk.ErrNotFound) {
			t.Errorf("the colleague reading the unshared request got %v, want not found", err)
		}
		mine, _ := d.MyRequests(ws.ctx, colleague.UserID)
		if len(mine) != 0 {
			t.Errorf("the colleague's own requests = %v, want none", requestKeys(mine))
		}
		own, _ := d.SharedRequests(ws.ctx, reporter.UserID)
		if len(own) != 0 {
			t.Errorf("the reporter's shared list = %v, want their own left to their requests", requestKeys(own))
		}
	})

	t.Run("nobody outside the company sees either", func(t *testing.T) {
		for who, outsider := range map[string]issue.Actor{"another company's customer": rival, "a customer in no company": stranger} {
			for _, key := range []string{shared.Key, private.Key} {
				if _, err := d.Request(ws.ctx, key, outsider.UserID); !errors.Is(err, desk.ErrNotFound) {
					t.Errorf("%s reading %s got %v, want not found", who, key, err)
				}
			}
			listed, _ := d.SharedRequests(ws.ctx, outsider.UserID)
			for _, i := range listed {
				if i.Key == shared.Key || i.Key == private.Key {
					t.Errorf("%s is shown %s", who, i.Key)
				}
			}
		}
		if _, err := d.Request(ws.ctx, theirs.Key, colleague.UserID); !errors.Is(err, desk.ErrNotFound) {
			t.Errorf("Globex reading what Initech shared got %v, want not found", err)
		}
	})

	t.Run("a colleague replies but sees no note and is not made a follower", func(t *testing.T) {
		if _, _, err := ws.issues.AddNote(ws.ctx, shared.Key, issue.TextDocument("their gateway is misconfigured"), ws.actor); err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.Reply(ws.ctx, shared.Key, "Same here since nine.", colleague); err != nil {
			t.Fatalf("the colleague's reply: %v", err)
		}
		read, err := d.Request(ws.ctx, shared.Key, colleague.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if len(read.Comments) != 1 || read.Comments[0].Internal || !strings.Contains(issue.PlainText(read.Comments[0].Body), "Same here") {
			t.Errorf("the colleague sees %+v, want their reply and no note", read.Comments)
		}
		watchers, err := ws.issues.Watchers(ws.ctx, shared.Key)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range watchers {
			if w.UserID == colleague.UserID {
				t.Error("replying made the colleague a follower, who would then be mailed")
			}
		}
		if err := d.MayFollow(ws.ctx, shared.Key, colleague.UserID); !errors.Is(err, desk.ErrNotYourRequest) {
			t.Errorf("the colleague adding a follower got %v, want only the reporter to", err)
		}
	})

	t.Run("through an open door there is no company", func(t *testing.T) {
		door := desk.WithScope(ws.ctx, desk.Scope{ProjectID: p.ID, ProjectKey: p.Key})
		if _, err := d.Request(door, shared.Key, colleague.UserID); !errors.Is(err, desk.ErrNotFound) {
			t.Errorf("an open door session reading a colleague's request got %v, want not found", err)
		}
		if listed, _ := d.SharedRequests(door, colleague.UserID); len(listed) != 0 {
			t.Errorf("an open door session is shown %v", requestKeys(listed))
		}
		types, _ := d.RequestTypes(ws.ctx, p.Key)
		if _, _, err := d.Raise(door, desk.RaiseInput{RequestTypeID: types[0].ID, Summary: "Shared on trust", ShareWithCompany: true}, colleague); !errors.Is(err, desk.ErrNoCompany) {
			t.Errorf("sharing through an open door got %v, want no company to share with", err)
		}
	})

	t.Run("agents narrow the queue to a company", func(t *testing.T) {
		page, err := d.Queue(ws.ctx, p.Key, desk.QueueAll, &globex.ID, ws.actor.UserID, issue.Page{})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, row := range page.Rows {
			got[row.Issue.Key] = row.Issue.Company.Name
		}
		if len(got) != 2 || got[shared.Key] != "Globex" || got[private.Key] != "Globex" {
			t.Errorf("Globex's queue = %v, want its two requests", got)
		}
		page, _ = d.Queue(ws.ctx, p.Key, desk.QueueAll, &initech.ID, ws.actor.UserID, issue.Page{})
		if len(page.Rows) != 1 || page.Rows[0].Issue.Key != theirs.Key {
			t.Errorf("Initech's queue = %+v, want its one request", page.Rows)
		}
	})

	t.Run("a member who leaves stops seeing at once", func(t *testing.T) {
		if _, err := d.RemoveCompanyMember(ws.ctx, globex.ID, colleague.UserID); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Request(ws.ctx, shared.Key, colleague.UserID); !errors.Is(err, desk.ErrNotFound) {
			t.Errorf("a former colleague reading the request got %v, want not found", err)
		}
		if _, err := d.RemoveCompanyMember(ws.ctx, globex.ID, colleague.UserID); !errors.Is(err, desk.ErrNotAMember) {
			t.Errorf("taking them out twice got %v, want not a member", err)
		}
		still, err := ws.issues.ByKey(ws.ctx, shared.Key)
		if err != nil || still.Company == nil || !still.SharedWithCompany {
			t.Errorf("the request after a colleague left = %+v %v, want it still Globex's and shared", still, err)
		}
	})

	t.Run("a request moved to another desk leaves its company", func(t *testing.T) {
		moved, _, err := ws.issues.Move(ws.ctx, private.Key, issue.MoveInput{ProjectKey: elsewhere.Key}, ws.actor)
		if err != nil {
			t.Fatalf("move: %v", err)
		}
		if moved.Company != nil || moved.SharedWithCompany {
			t.Errorf("the moved request kept %+v shared=%v", moved.Company, moved.SharedWithCompany)
		}
	})

	t.Run("a company that goes takes the sharing with it", func(t *testing.T) {
		if _, err := d.DeleteCompany(ws.ctx, globex.ID); err != nil {
			t.Fatal(err)
		}
		after, err := ws.issues.ByKey(ws.ctx, shared.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Company != nil || after.SharedWithCompany {
			t.Errorf("after the company went the request is %+v shared=%v", after.Company, after.SharedWithCompany)
		}
		if _, err := d.Request(ws.ctx, shared.Key, reporter.UserID); err != nil {
			t.Errorf("the reporter keeps their request: %v", err)
		}
		if _, err := d.DeleteCompany(ws.ctx, globex.ID); !errors.Is(err, desk.ErrCompanyNotFound) {
			t.Errorf("deleting it twice got %v", err)
		}
	})
}

// customerAt adds a customer at a domain of the test's own, removed at the end.
func customerAt(t *testing.T, h *harness, prefix, domain string) issue.Actor {
	t.Helper()
	id := h.joinExisting(t, unique(prefix)+"@"+domain, "customer")
	return issue.Actor{UserID: id, OrgRole: auth.RoleCustomer}
}

// A customer at a company's domain joins it with their first request at the
// desk, and only then: one taken out, or already there, stays where they are.
func TestACustomerJoinsTheCompanyOfTheirDomainOnTheirFirstRequest(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "domains")
	d, p := ws.aDesk(t, h, "Support")
	domain := strings.ToLower(uniqueKey("UMB")) + ".test"
	early := customerAt(t, h, "early", domain)
	raiseAt(t, ws, d, p.Key, "Before there were companies", false, early)

	umbrella, _, err := d.CreateCompany(ws.ctx, p.Key, desk.CompanyInput{Name: named("Umbrella"), Domains: &[]string{"@" + strings.ToUpper(domain)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(umbrella.Domains) != 1 || umbrella.Domains[0] != domain {
		t.Errorf("domains = %v, want %s written the one way", umbrella.Domains, domain)
	}
	ada := customerAt(t, h, "ada", domain)
	outside := customerAt(t, h, "outside", "elsewhere-"+domain)

	desks, err := d.Desks(ws.ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	for _, each := range desks {
		if each.ProjectKey == p.Key && (each.Company == nil || each.Company.Name != "Umbrella") {
			t.Errorf("before her first request the desk offers Ada %+v, want Umbrella to share with", each.Company)
		}
	}
	first := raiseAt(t, ws, d, p.Key, "The lab is flooded", true, ada)
	if first.Company == nil || first.Company.ID != umbrella.ID || !first.SharedWithCompany {
		t.Errorf("Ada's first request = %+v shared=%v, want Umbrella's", first.Company, first.SharedWithCompany)
	}
	members := func() map[uuid.UUID]bool {
		companies, err := d.Companies(ws.ctx, p.Key)
		if err != nil || len(companies) != 1 {
			t.Fatalf("companies = %+v %v", companies, err)
		}
		in := map[uuid.UUID]bool{}
		for _, m := range companies[0].Members {
			in[m.UserID] = true
		}
		return in
	}
	if in := members(); !in[ada.UserID] || in[early.UserID] || in[outside.UserID] {
		t.Errorf("members = %v, want Ada alone", in)
	}

	again := raiseAt(t, ws, d, p.Key, "Still here", false, early)
	if again.Company != nil {
		t.Errorf("a customer with earlier requests joined %+v by domain", again.Company)
	}
	if _, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: *first.RequestTypeID, Summary: "Share it", ShareWithCompany: true}, outside); !errors.Is(err, desk.ErrNoCompany) {
		t.Errorf("sharing from no company got %v", err)
	}

	if _, err := d.RemoveCompanyMember(ws.ctx, umbrella.ID, ada.UserID); err != nil {
		t.Fatal(err)
	}
	raiseAt(t, ws, d, p.Key, "After I was taken out", false, ada)
	if members()[ada.UserID] {
		t.Error("a customer taken out of the company joined it again by domain")
	}

	_, _, err = d.CreateCompany(ws.ctx, p.Key, desk.CompanyInput{Name: named("Umbrella Labs"), Domains: &[]string{domain}})
	var taken *desk.DomainTakenError
	if !errors.As(err, &taken) || taken.Company != "Umbrella" {
		t.Errorf("a second company taking the domain got %v, want Umbrella named", err)
	}
	if _, _, err := d.CreateCompany(ws.ctx, p.Key, desk.CompanyInput{Name: named("umbrella")}); !errors.Is(err, desk.ErrCompanyNameTaken) {
		t.Errorf("a second Umbrella got %v", err)
	}
	if _, _, err := d.CreateCompany(ws.ctx, p.Key, desk.CompanyInput{Name: named("  ")}); !errors.Is(err, desk.ErrCompanyName) {
		t.Errorf("a nameless company got %v", err)
	}
	if _, _, err := d.CreateCompany(ws.ctx, ws.project.Key, desk.CompanyInput{Name: named("Soft")}); !errors.Is(err, desk.ErrNotAServiceProject) {
		t.Errorf("a company of a software project got %v", err)
	}
}

func wantRefused(t *testing.T, what string, code string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Errorf("%s: got %v, want the database to refuse it with %s", what, err, code)
	}
}

// What the api refuses the database refuses too, straight through SQL.
func TestTheDatabaseRefusesWhatNoCompanyCouldBe(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "companysql")
	d, p := ws.aDesk(t, h, "Support")
	_, other := ws.aDesk(t, h, "Other")
	member := ws.customerOf(t, h, "member")
	globex := aCompany(t, ws, d, p.Key, "Globex", member)
	initech := aCompany(t, ws, d, p.Key, "Initech")
	otherCompany := aCompany(t, ws, d, other.Key, "Elsewhere")
	raised := raiseAt(t, ws, d, p.Key, "Shared", true, member)
	ctx := context.Background()
	const (
		checkViolation  = "23514"
		foreignKey      = "23503"
		uniqueViolation = "23505"
	)

	_, err := h.super.Exec(ctx, `UPDATE issue SET company_id = $2 WHERE id = $1`, raised.ID, otherCompany.ID)
	wantRefused(t, "an issue naming another desk's company", foreignKey, err)

	_, err = h.super.Exec(ctx, `INSERT INTO customer_company_member (company_id, project_id, user_id) VALUES ($1, $2, $3)`, globex.ID, p.ID, ws.actor.UserID)
	wantRefused(t, "somebody who works here in a company", checkViolation, err)

	_, err = h.super.Exec(ctx, `INSERT INTO customer_company_member (company_id, project_id, user_id) VALUES ($1, $2, $3)`, initech.ID, p.ID, member.UserID)
	wantRefused(t, "a customer in two companies of one desk", uniqueViolation, err)

	_, err = h.super.Exec(ctx, `INSERT INTO customer_company_member (company_id, project_id, user_id) VALUES ($1, $2, $3)`, globex.ID, other.ID, ws.customerOf(t, h, "astray").UserID)
	wantRefused(t, "a member said to be of another desk than their company", foreignKey, err)

	plain, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: p.Key, Summary: "From an agent"}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.super.Exec(ctx, `UPDATE issue SET shared_with_company = true WHERE id = $1`, plain.ID)
	wantRefused(t, "a request shared with no company", checkViolation, err)

	_, err = h.super.Exec(ctx, `INSERT INTO customer_company (project_id, name) VALUES ($1, 'Soft')`, ws.project.ID)
	wantRefused(t, "a company of a software project", checkViolation, err)

	_, err = h.super.Exec(ctx, `UPDATE customer_company SET domains = '{"globex.test"}' WHERE id = $1`, globex.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.super.Exec(ctx, `UPDATE customer_company SET domains = '{"globex.test"}' WHERE id = $1`, initech.ID)
	wantRefused(t, "two companies of a desk taking one domain", checkViolation, err)

	_, err = h.super.Exec(ctx, `UPDATE customer_company SET project_id = $2 WHERE id = $1`, initech.ID, other.ID)
	wantRefused(t, "a company moved to another desk", checkViolation, err)

	_, err = h.super.Exec(ctx, `UPDATE customer_company SET domains = '{"not a domain"}' WHERE id = $1`, initech.ID)
	wantRefused(t, "a domain that is not one", checkViolation, err)

	// Somebody who starts working here leaves their companies, however it happens.
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET standing = 'member' WHERE id = $1`, member.UserID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM customer_company_member WHERE user_id = $1`, member.UserID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("a customer who became a member is still in %d companies", left)
	}
}

// A customer's companies are in their export, and an erasure takes them out.
func TestACustomersCompaniesAreTheirsToExportAndErase(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "companyprivacy")
	d, p := ws.aDesk(t, h, "Support")
	customer := ws.customerOf(t, h, "exported")
	aCompany(t, ws, d, p.Key, "Globex", customer)
	h.erasable(customer.UserID.String())

	svc := privacy.NewService(h.cluster)
	exported, err := svc.Export(ws.ctx, customer.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Companies) != 1 || exported.Companies[0].Company != "Globex" || exported.Companies[0].Desk != "Support" {
		t.Errorf("the export's companies = %+v, want Globex at Support", exported.Companies)
	}
	if err := svc.Erase(ws.ctx, customer.UserID, customer.UserID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM customer_company_member WHERE user_id = $1`, customer.UserID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("an erased customer is still in %d companies", left)
	}
}

// Every company operation over the API, an agent who does not administer the
// desk refused the writes, and a colleague reading what was shared.
func TestCompaniesOverTheAPI(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	key := h.projectKey(t, "CMP")
	api := newAPIServer(t, h)
	owner := api.client(t)
	ownerEmail := h.person(t, "companyowner", auth.RoleOwner)
	owner.signIn(t, ownerEmail)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Help", "key": key, "template": "service-desk"}), http.StatusCreated, "desk")
	path := "/api/v1/projects/" + key + "/companies"

	want(t, owner.post(path, map[string]any{"name": ""}), http.StatusUnprocessableEntity, "a nameless company")
	want(t, owner.post(path, map[string]any{"name": "Globex", "domains": []string{"not a domain"}}), http.StatusUnprocessableEntity, "a bad domain")
	made := want(t, owner.post(path, map[string]any{"name": "Globex Corp", "domains": []string{"globex.test"}}), http.StatusCreated, "a company")
	companyID := idOf(t, made, "company")
	want(t, owner.post(path, map[string]any{"name": "globex corp"}), http.StatusConflict, "the same name again")
	want(t, owner.post(path, map[string]any{"name": "Globex Labs", "domains": []string{"globex.test"}}), http.StatusConflict, "the same domain again")
	if renamed := obj(t, want(t, owner.patch("/api/v1/companies/"+companyID, map[string]any{"name": "Globex"}), http.StatusOK, "renaming"), "company"); renamed["name"] != "Globex" {
		t.Errorf("renamed = %v", renamed)
	}
	want(t, owner.patch("/api/v1/companies/"+uuid.NewString(), map[string]any{"name": "Nobody"}), http.StatusNotFound, "a company that is not there")

	agent := api.asRole(t, h, owner, key, perm.User)
	want(t, agent.get(path), http.StatusOK, "an agent reads the companies")
	want(t, agent.post(path, map[string]any{"name": "Mine"}), http.StatusForbidden, "an agent adding a company")
	want(t, agent.patch("/api/v1/companies/"+companyID, map[string]any{"name": "Mine"}), http.StatusForbidden, "an agent renaming it")
	want(t, agent.post("/api/v1/companies/"+companyID+"/members", map[string]any{"email": h.email(t, "sneaked")}), http.StatusForbidden, "an agent adding a member")
	want(t, agent.delete("/api/v1/companies/"+companyID), http.StatusForbidden, "an agent removing it")

	reporterEmail := h.person(t, "reporter", auth.RoleCustomer)
	colleagueEmail := h.person(t, "colleague", auth.RoleCustomer)
	strangerEmail := h.person(t, "stranger", auth.RoleCustomer)
	for _, email := range []string{reporterEmail, colleagueEmail} {
		want(t, owner.post("/api/v1/companies/"+companyID+"/members", map[string]any{"email": email}), http.StatusCreated, "putting a customer in")
	}
	want(t, owner.post("/api/v1/companies/"+companyID+"/members", map[string]any{"email": ownerEmail}), http.StatusConflict, "putting somebody who works here in")
	want(t, owner.post("/api/v1/companies/"+companyID+"/members", map[string]any{"email": "not an address"}), http.StatusUnprocessableEntity, "a member who is not an address")
	newcomer := h.email(t, "newcomer")
	added := obj(t, want(t, owner.post("/api/v1/companies/"+companyID+"/members", map[string]any{"email": newcomer}), http.StatusCreated, "an address with no account"), "company")
	var newcomerID string
	for _, m := range added["members"].([]any) {
		if m.(map[string]any)["email"] == newcomer {
			newcomerID = m.(map[string]any)["userId"].(string)
		}
	}
	if newcomerID == "" {
		t.Fatalf("the newcomer is not among %v", added["members"])
	}
	want(t, owner.delete("/api/v1/companies/"+companyID+"/members/"+newcomerID), http.StatusNoContent, "taking the newcomer out")
	want(t, owner.delete("/api/v1/companies/"+companyID+"/members/"+newcomerID), http.StatusNotFound, "taking them out twice")
	h.waitForPrimary(t)

	reporter := api.client(t)
	reporter.signIn(t, reporterEmail)
	var typeID any
	for _, raw := range list(t, want(t, reporter.get("/api/v1/portal/desks"), http.StatusOK, "the desks"), "desks") {
		if d := raw.(map[string]any); d["projectKey"] == key {
			if company, _ := d["company"].(map[string]any); company["name"] != "Globex" {
				t.Errorf("the desk offers the reporter %v to share with, want Globex", d["company"])
			}
			typeID = d["requestTypes"].([]any)[0].(map[string]any)["id"]
		}
	}
	raised := obj(t, want(t, reporter.post("/api/v1/portal/requests", map[string]any{"requestTypeId": typeID, "summary": "The VPN is down", "shareWithCompany": true}), http.StatusCreated, "a shared request"), "request")
	requestKey := raised["key"].(string)
	if raised["sharedWithCompany"] != true || raised["company"].(map[string]any)["name"] != "Globex" {
		t.Errorf("the request = %v, want it shared with Globex", raised)
	}
	h.waitForPrimary(t)

	colleague := api.client(t)
	colleague.signIn(t, colleagueEmail)
	listed := want(t, colleague.get("/api/v1/portal/requests"), http.StatusOK, "the colleague's lists")
	if mine := list(t, listed, "requests"); len(mine) != 0 {
		t.Errorf("the colleague's own requests = %v", mine)
	}
	if shared := list(t, listed, "shared"); len(shared) != 1 || shared[0].(map[string]any)["key"] != requestKey {
		t.Errorf("shared with the colleague = %v, want %s", shared, requestKey)
	}
	want(t, colleague.get("/api/v1/portal/requests/"+requestKey), http.StatusOK, "the colleague reads it")
	want(t, colleague.post("/api/v1/portal/requests/"+requestKey+"/replies", map[string]any{"text": "Same here."}), http.StatusCreated, "the colleague replies")
	want(t, colleague.upload("/api/v1/portal/requests/"+requestKey+"/attachments", "file", "trace.txt", "text/plain", []byte("timeout")), http.StatusCreated, "the colleague attaches")
	want(t, colleague.post("/api/v1/portal/requests/"+requestKey+"/watchers", map[string]any{"email": h.email(t, "friend")}), http.StatusForbidden, "the colleague adding a follower")

	stranger := api.client(t)
	stranger.signIn(t, strangerEmail)
	want(t, stranger.get("/api/v1/portal/requests/"+requestKey), http.StatusNotFound, "a customer outside the company")

	h.waitForPrimary(t)
	queue := want(t, owner.get("/api/v1/projects/"+key+"/queue?filter=all&company="+companyID), http.StatusOK, "Globex's queue")
	if rows := list(t, queue, "rows"); len(rows) != 1 || rows[0].(map[string]any)["issue"].(map[string]any)["key"] != requestKey {
		t.Errorf("Globex's queue = %v, want the shared request", rows)
	}
	want(t, owner.get("/api/v1/projects/"+key+"/queue?company=nobody"), http.StatusBadRequest, "a company that is not an id")
	found := want(t, owner.get("/api/v1/issues?q="+urlQuery(`project = `+key+` AND company = Globex`)), http.StatusOK, "a query naming the company")
	if !strings.Contains(found.Raw, requestKey) {
		t.Errorf("company = Globex did not find %s: %s", requestKey, found.Raw)
	}
	other := want(t, owner.get("/api/v1/issues?q="+urlQuery(`project = `+key+` AND company = Initech`)), http.StatusOK, "a query naming another company")
	if strings.Contains(other.Raw, requestKey) {
		t.Errorf("company = Initech found Globex's request: %s", other.Raw)
	}

	want(t, owner.delete("/api/v1/companies/"+companyID), http.StatusNoContent, "removing the company")
	want(t, owner.delete("/api/v1/companies/"+companyID), http.StatusNotFound, "removing it twice")
	h.waitForPrimary(t)
	want(t, colleague.get("/api/v1/portal/requests/"+requestKey), http.StatusNotFound, "the colleague once the company is gone")
	if companies := list(t, want(t, owner.get(path), http.StatusOK, "the companies"), "companies"); len(companies) != 0 {
		t.Errorf("companies left = %v", companies)
	}
}
