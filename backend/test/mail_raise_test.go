//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// mailFileLimit is what the desk's file store takes in this test, so one file
// of the mail fits and one does not.
const mailFileLimit = 64

// mailWithFiles is a new mail with words, a log that fits and a dump that does not.
func mailWithFiles(from, to, subject, id string) string {
	return strings.Join([]string{
		"From: Gus <" + from + ">", "To: " + to, "Subject: " + subject, "Message-ID: <" + id + ">",
		`Content-Type: multipart/mixed; boundary="b"`, "",
		"--b", "Content-Type: text/plain; charset=utf-8", "",
		"It jams on every page.", "",
		"--b", "Content-Type: text/plain", `Content-Disposition: attachment; filename="printer.log"`, "",
		"jam at tray 2",
		"--b", "Content-Type: application/octet-stream", `Content-Disposition: attachment; filename="dump.bin"`, "",
		strings.Repeat("x", 2*mailFileLimit),
		"--b--", "",
	}, "\r\n")
}

// mailRig is a desk that raises requests from mail, and a reader of its box.
type mailRig struct {
	h       *harness
	ws      *workspace
	d       *desk.Service
	p       *project.Project
	mailer  *fakeMailer
	inbox   *fakeInbox
	reader  *desk.Inbound
	address string
	run     string
	// mailType is what the desk's new mail becomes.
	mailType desk.RequestType
}

func newMailRig(t *testing.T, h *harness, ws *workspace) *mailRig {
	t.Helper()
	d, p := ws.aDesk(t, h, "Raise desk")
	d.WithAttachments(attachment.NewService(h.cluster, h.attachmentStore(t), ws.issues).WithMaxSize(mailFileLimit))
	d.RepliesByMail(true).WithInbox(inboxAddress)
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	r := &mailRig{h: h, ws: ws, d: d, p: p, mailer: &fakeMailer{}, inbox: &fakeInbox{mail: map[string]string{}},
		address: desk.Address(inboxAddress, p.Key), run: uuid.NewString()[:8], mailType: requestTypeNamed(t, types, "Ask a question")}
	if _, _, err := ws.projects.Update(ws.ctx, p.Key, project.UpdateInput{MailRequestTypeID: ptr(&r.mailType.ID)}); err != nil {
		t.Fatal(err)
	}
	r.reader = desk.NewInbound(h.cluster, d, ws.issues, r.mailer, r.inbox,
		desk.InboundConfig{Address: inboxAddress, From: "Armature <no-reply@armature.test>", AppURL: "http://app.test"},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	return r
}

// id is a message id of this run; ids are unique in the table for good.
func (r *mailRig) id(name string) string { return name + "-" + r.run + "@example.com" }

// read hands the reader one mail and forgets what was sent before.
func (r *mailRig) read(t *testing.T, uid, raw string) {
	t.Helper()
	r.mailer.sent = nil
	r.inbox.mail = map[string]string{uid: raw}
	if err := r.reader.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (r *mailRig) outcome(t *testing.T, name string) (string, *uuid.UUID) {
	t.Helper()
	var (
		got     string
		issueID *uuid.UUID
	)
	if err := r.h.super.QueryRow(context.Background(), `SELECT outcome, issue_id FROM inbound_mail WHERE message_id = $1`, r.id(name)).Scan(&got, &issueID); err != nil {
		t.Fatalf("no row for %s: %v", name, err)
	}
	return got, issueID
}

// requests counts what the desk holds, which is all this test's.
func (r *mailRig) requests(t *testing.T) int {
	t.Helper()
	var n int
	if err := r.h.super.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE project_id = $1`, r.p.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A new mail to a desk's own address becomes a request of the desk's mail type,
// raised by its sender as a customer, with its words and its files.
func TestANewMailToADeskRaisesARequest(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	newcomer := h.email(t, "newcomer")
	ws := h.newProject(t, "mailraise")
	r := newMailRig(t, h, ws)

	var raised *issue.Issue
	t.Run("the mail becomes a request by its sender, with its words and the files that fit", func(t *testing.T) {
		r.read(t, "n1", mailWithFiles(newcomer, r.address, "The printer jams", r.id("one")))
		got, issueID := r.outcome(t, "one")
		if got != string(desk.Raised) || issueID == nil {
			t.Fatalf("outcome %s on %v, want raised on the new request", got, issueID)
		}
		var key string
		if err := h.super.QueryRow(context.Background(), `SELECT p.key || '-' || i.key_num FROM issue i JOIN project p ON p.id = i.project_id WHERE i.id = $1`, *issueID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		found, err := ws.issues.ByKey(ws.ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		raised = found
		if found.ProjectID != r.p.ID || found.Summary != "The printer jams" || issue.PlainText(found.Description) != "It jams on every page." {
			t.Errorf("request = %s %q %q, want the subject and the words at the desk", found.Key, found.Summary, issue.PlainText(found.Description))
		}
		if found.RequestTypeID == nil || *found.RequestTypeID != r.mailType.ID {
			t.Errorf("request type = %v, want the desk's mail type %s", found.RequestTypeID, r.mailType.ID)
		}
		var (
			reporter string
			standing *string
			password *string
		)
		if err := h.super.QueryRow(context.Background(), `
			SELECT u.email, u.standing, u.password_hash FROM issue i JOIN app_user u ON u.id = i.reporter_id WHERE i.id = $1`, found.ID).
			Scan(&reporter, &standing, &password); err != nil {
			t.Fatal(err)
		}
		if reporter != newcomer || standing == nil || *standing != "customer" || password != nil {
			t.Errorf("reporter %s, standing %v, password set %v; want the sender as a customer without a password", reporter, standing, password != nil)
		}
		files, err := attachment.NewService(h.cluster, h.attachmentStore(t), ws.issues).List(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 || files[0].FileName != "printer.log" || files[0].Uploader == nil || files[0].Uploader.ID != found.Reporter.ID {
			t.Errorf("files = %+v, want the log alone, put there by the sender", files)
		}
		notes, _ := ws.issues.Comments(ws.ctx, found.Key, true)
		if len(notes) != 1 || !notes[0].Internal || !strings.Contains(issue.PlainText(notes[0].Body), "dump.bin") {
			t.Errorf("comments = %+v, want one internal note naming the file left out", notes)
		}
		if len(r.mailer.sent) != 0 {
			t.Errorf("the reader wrote %+v; the receipt is the notifier's", r.mailer.sent)
		}
	})
	if raised == nil {
		t.FailNow()
	}

	t.Run("the same mail again raises nothing more", func(t *testing.T) {
		before := r.requests(t)
		r.read(t, "n1-again", mailWithFiles(newcomer, r.address, "The printer jams", r.id("one")))
		if r.requests(t) != before {
			t.Error("a mail delivered twice raised two requests")
		}
	})

	t.Run("the new customer's answer is a reply on the request", func(t *testing.T) {
		r.read(t, "n2", rawMail(newcomer, inboxAddress, "Re: ["+raised.Key+"] we have your request", r.id("two"), "Tray 2 again."))
		if got, _ := r.outcome(t, "two"); got != string(desk.Commented) {
			t.Fatalf("outcome = %s, want the reply taken", got)
		}
		comments, _ := ws.issues.Comments(ws.ctx, raised.Key, false)
		if len(comments) != 1 || issue.PlainText(comments[0].Body) != "Tray 2 again." || comments[0].Author == nil || comments[0].Author.ID != raised.Reporter.ID {
			t.Errorf("public comments = %+v, want the reporter's reply", comments)
		}
	})

	t.Run("the portal shows the desk's address", func(t *testing.T) {
		desks, err := r.d.Desks(ws.ctx, ws.customerOf(t, h, "looker"))
		if err != nil {
			t.Fatal(err)
		}
		shown := false
		for _, found := range desks {
			if found.ProjectID == r.p.ID {
				shown = found.MailAddress == r.address
			}
		}
		if !shown {
			t.Errorf("desks = %+v, want %s on the desk", desks, r.address)
		}
	})
}

// What raises no request: a machine's mail, a forged sender, an agent, a domain
// the desk does not trust, the bare inbox and a desk that names no mail type.
func TestMailThatRaisesNoRequest(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	sender := h.email(t, "sender")
	ws := h.newProject(t, "mailnone")
	r := newMailRig(t, h, ws)
	var agentEmail string
	if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, ws.actor.UserID).Scan(&agentEmail); err != nil {
		t.Fatal(err)
	}

	t.Run("a machine's mail raises nothing and is not answered", func(t *testing.T) {
		r.read(t, "m1", rawMail(sender, r.address, "Out of office", r.id("one"), "Away until Monday.", "Auto-Submitted: auto-replied"))
		r.read(t, "m2", rawMail("mailer-daemon@example.com", r.address, "Undeliverable", r.id("two"), "bounce"))
		for _, name := range []string{"one", "two"} {
			if got, _ := r.outcome(t, name); got != string(desk.Automatic) {
				t.Errorf("%s: outcome = %s, want automatic", name, got)
			}
		}
		if r.requests(t) != 0 || len(r.mailer.sent) != 0 {
			t.Errorf("requests %d, mails %+v; want none", r.requests(t), r.mailer.sent)
		}
	})

	t.Run("a sender the receiving server says is forged raises nothing", func(t *testing.T) {
		r.read(t, "m3", rawMail(sender, r.address, "Pay this", r.id("three"), "Invoice attached.",
			"Authentication-Results: mx.armature.test; spf=fail smtp.mailfrom=example.com; dkim=fail; dmarc=fail header.from=example.com"))
		if got, _ := r.outcome(t, "three"); got != string(desk.Refused) || r.requests(t) != 0 || len(r.mailer.sent) != 0 {
			t.Errorf("outcome %s, requests %d, mails %+v", got, r.requests(t), r.mailer.sent)
		}
	})

	t.Run("an agent's mail raises nothing and is answered as unmatched", func(t *testing.T) {
		r.read(t, "m4", rawMail(agentEmail, r.address, "For the customer", r.id("four"), "Please raise this."))
		if got, _ := r.outcome(t, "four"); got != string(desk.Unmatched) || r.requests(t) != 0 {
			t.Errorf("outcome %s, requests %d; want unmatched and none", got, r.requests(t))
		}
		if len(r.mailer.sent) != 1 || r.mailer.sent[0].To != agentEmail {
			t.Errorf("mails = %+v, want the one answer", r.mailer.sent)
		}
	})

	t.Run("a sender at a domain the desk does not trust is refused, and no account is made", func(t *testing.T) {
		if _, err := h.super.Exec(context.Background(), `UPDATE project SET trusted_domains = '{armature.test}' WHERE id = $1`, r.p.ID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = h.super.Exec(context.Background(), `UPDATE project SET trusted_domains = '{}' WHERE id = $1`, r.p.ID)
		})
		far := unique("far") + "@faraway.test"
		r.read(t, "m5", rawMail(far, r.address, "Let me in", r.id("five"), "Hello."))
		if got, _ := r.outcome(t, "five"); got != string(desk.Refused) || r.requests(t) != 0 {
			t.Errorf("outcome %s, requests %d; want refused and none", got, r.requests(t))
		}
		if len(r.mailer.sent) != 1 || !strings.Contains(r.mailer.sent[0].Body, "armature.test") || !strings.Contains(r.mailer.sent[0].Body, "faraway.test") {
			t.Errorf("mails = %+v, want one naming both domains", r.mailer.sent)
		}
		var accounts int
		_ = h.super.QueryRow(context.Background(), `SELECT count(*) FROM app_user WHERE email = $1`, far).Scan(&accounts)
		if accounts != 0 {
			t.Error("a refused sender was given an account")
		}
	})

	t.Run("mail to the bare inbox raises nothing and lists no desk", func(t *testing.T) {
		r.read(t, "m6", rawMail(sender, inboxAddress, "Something new", r.id("six"), "Help."))
		if got, _ := r.outcome(t, "six"); got != string(desk.Unmatched) || r.requests(t) != 0 {
			t.Errorf("outcome %s, requests %d; want unmatched and none", got, r.requests(t))
		}
		if len(r.mailer.sent) != 1 || strings.Contains(strings.ToLower(r.mailer.sent[0].Body), strings.ToLower(r.p.Key)) {
			t.Errorf("mails = %+v, want one answer that names no desk", r.mailer.sent)
		}
	})

	t.Run("a desk without a mail request type keeps the answer it gave", func(t *testing.T) {
		_, quiet := ws.aDesk(t, h, "Quiet desk")
		r.read(t, "m7", rawMail(sender, desk.Address(inboxAddress, quiet.Key), "Something new", r.id("seven"), "Help."))
		if got, _ := r.outcome(t, "seven"); got != string(desk.Unmatched) {
			t.Errorf("outcome = %s, want unmatched", got)
		}
		var there int
		_ = h.super.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE project_id = $1`, quiet.ID).Scan(&there)
		if there != 0 || len(r.mailer.sent) != 1 {
			t.Errorf("requests at the quiet desk %d, mails %+v; want none and one answer", there, r.mailer.sent)
		}
	})
}

// Only a desk names a mail request type, and only one of its own: the service
// says so in a sentence and the database refuses it through SQL too.
func TestAMailRequestTypeIsTheDesksOwn(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "mailtype")
	d, p := ws.aDesk(t, h, "Typed desk")
	_, other := ws.aDesk(t, h, "Other desk")
	mine, _ := d.RequestTypes(ws.ctx, p.Key)
	theirs, _ := d.RequestTypes(ws.ctx, other.Key)

	if _, _, err := ws.projects.Update(ws.ctx, p.Key, project.UpdateInput{MailRequestTypeID: ptr(&theirs[0].ID)}); !errors.Is(err, project.ErrRequestTypeElsewhere) {
		t.Errorf("another desk's type: %v, want ErrRequestTypeElsewhere", err)
	}
	if _, _, err := ws.projects.Update(ws.ctx, ws.project.Key, project.UpdateInput{MailRequestTypeID: ptr(&mine[0].ID)}); !errors.Is(err, project.ErrRequestTypeElsewhere) {
		t.Errorf("a software project: %v, want ErrRequestTypeElsewhere", err)
	}
	_, err := h.super.Exec(context.Background(), `UPDATE project SET mail_request_type_id = $2 WHERE id = $1`, p.ID, theirs[0].ID)
	if err == nil || !strings.Contains(err.Error(), "another project") {
		t.Errorf("another desk's type through SQL: %v", err)
	}
	_, err = h.super.Exec(context.Background(), `UPDATE project SET mail_request_type_id = $2 WHERE key = $1`, ws.project.Key, mine[0].ID)
	if err == nil || !strings.Contains(err.Error(), "only a service desk") {
		t.Errorf("a software project through SQL: %v", err)
	}

	updated, _, err := ws.projects.Update(ws.ctx, p.Key, project.UpdateInput{MailRequestTypeID: ptr(&mine[0].ID)})
	if err != nil || updated.MailRequestTypeID == nil || *updated.MailRequestTypeID != mine[0].ID {
		t.Fatalf("its own type: %+v, %v", updated, err)
	}
	if _, err := d.DeleteRequestType(ws.ctx, mine[0].ID); err != nil {
		t.Fatal(err)
	}
	after, err := ws.projects.ByKey(ws.ctx, p.Key)
	if err != nil || after.MailRequestTypeID != nil {
		t.Errorf("after its type went the desk raises %v by mail (%v), want nothing", after.MailRequestTypeID, err)
	}
}

// The desk's settings name the type over the API, and show the address to mail.
func TestADeskIsMailedAtItsOwnAddress(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	api.api.Desk.RepliesByMail(true).WithInbox("Support <" + inboxAddress + ">")
	owner := api.client(t)
	owner.signIn(t, h.person(t, "mailing-owner", auth.RoleOwner))
	key, code := h.projectKey(t, "MD"), h.projectKey(t, "MC")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Mail desk", "key": key, "template": "service-desk"}), http.StatusCreated, "a desk")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Code", "key": code}), http.StatusCreated, "a software project")
	types := list(t, want(t, owner.get("/api/v1/projects/"+key+"/request-types"), http.StatusOK, "request types"), "requestTypes")
	typeID := types[0].(map[string]any)["id"].(string)

	address := obj(t, want(t, owner.get("/api/v1/projects/"+key+"/mail-address"), http.StatusOK, "the address"))["address"]
	if address != "support+"+strings.ToLower(key)+"@armature.test" {
		t.Errorf("address = %v", address)
	}
	set := obj(t, want(t, owner.patch("/api/v1/projects/"+key, map[string]any{"mailRequestTypeId": typeID}), http.StatusOK, "name the type"), "project")
	if set["mailRequestTypeId"] != typeID {
		t.Errorf("project = %+v, want the type named", set)
	}
	want(t, owner.patch("/api/v1/projects/"+code, map[string]any{"mailRequestTypeId": typeID}), http.StatusUnprocessableEntity, "another project's type")
	cleared := obj(t, want(t, owner.patch("/api/v1/projects/"+key, map[string]any{"mailRequestTypeId": nil}), http.StatusOK, "stop raising by mail"), "project")
	if _, still := cleared["mailRequestTypeId"]; still {
		t.Errorf("project = %+v, want no mail type", cleared)
	}
}
