//go:build integration

package test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/mailin"
)

// A relay and a mailbox whose certificate nobody signed refuse the real
// clients, until ARMATURE_MAIL_INSECURE_TLS tells them to take it anyway.
func TestMailThroughAnUntrustedCertificateNeedsInsecureTLS(t *testing.T) {
	smtpAddr, popAddr := os.Getenv("ARMATURE_TEST_TLS_SMTP_ADDR"), os.Getenv("ARMATURE_TEST_TLS_POP3_ADDR")
	if smtpAddr == "" || popAddr == "" {
		t.Skip("ARMATURE_TEST_TLS_SMTP_ADDR and ARMATURE_TEST_TLS_POP3_ADDR are not set")
	}
	ctx := context.Background()
	run := uuid.NewString()[:8]
	refused := fmt.Sprintf("tls-refused-%s@armature.test", run)
	accepted := fmt.Sprintf("tls-accepted-%s@armature.test", run)
	from := "Armature <no-reply@armature.test>"

	err := mail.SMTPMailer{Addr: smtpAddr, From: from}.Send(ctx, mail.Mail{To: refused, Subject: "refused", Body: run})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("an unsigned certificate should be refused, got %v", err)
	}
	if err := (mail.SMTPMailer{Addr: smtpAddr, From: from, InsecureTLS: true}).Send(ctx, mail.Mail{To: accepted, Subject: "accepted", Body: run}); err != nil {
		t.Fatalf("with InsecureTLS the relay should take the mail: %v", err)
	}

	box := mailin.POP3{Addr: popAddr, User: os.Getenv("ARMATURE_POP3_USER"), Password: os.Getenv("ARMATURE_POP3_PASSWORD"), TLS: true}
	none := func(string) bool { return false }
	if err := box.Poll(ctx, none, func(string, []byte) bool { return false }); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("an unsigned certificate on the mailbox should be refused, got %v", err)
	}

	box.InsecureTLS = true
	var got []string
	for deadline := time.Now().Add(10 * time.Second); len(got) == 0 && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		err := box.Poll(ctx, none, func(uid string, raw []byte) bool {
			m, err := mailin.Parse(raw)
			if err != nil {
				return false
			}
			for _, r := range m.Recipients {
				if r == accepted || r == refused {
					got = append(got, r)
					return true
				}
			}
			return false
		})
		if err != nil {
			t.Fatalf("with InsecureTLS the mailbox should be read: %v", err)
		}
	}
	if len(got) != 1 || got[0] != accepted {
		t.Errorf("the mailbox held %v, want only the mail sent with InsecureTLS", got)
	}
}
