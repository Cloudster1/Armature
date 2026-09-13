//go:build integration

package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/tenant"
)

// Testing and redelivering a webhook read back the delivery they just wrote,
// so they must read it where it was written, however far a replica lags.
func TestAWebhookTestReadsTheDeliveryItJustWrote(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	orgID := uuid.MustParse(principalField(t, c.signup(t, h, "hooktest"), "principal", "org", "id").(string))

	sink := &receiver{status: http.StatusOK}
	server := httptest.NewServer(sink)
	defer server.Close()
	hook := want(t, c.post("/api/v1/webhooks", map[string]any{"name": "Pager", "url": server.URL, "topics": []string{"*"}}), http.StatusCreated, "a webhook")
	hookID := idOf(t, hook, "webhook")

	// The replica holds everything up to here, so the caller's reads may go
	// there, and then it stops: whatever the test writes next is primary only.
	ctx := tenant.WithOrg(context.Background(), tenant.Org{ID: orgID})
	warmup, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, 'webhook.warmup', 'org')`, orgID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !h.waitForReplica(t, warmup, 10*time.Second) {
		t.Fatal("the replica never caught up")
	}
	// The router trusts a replica only as far as its last health check saw it,
	// so wait until that check has seen the warmup too.
	routable := func() bool {
		for _, r := range h.cluster.Stats().Replicas {
			if r.Healthy && r.ReplayLSN >= warmup {
				return true
			}
		}
		return false
	}
	for deadline := time.Now().Add(10 * time.Second); !routable(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the cluster never saw the replica catch up")
		}
	}
	h.pauseReplay(t)

	tested := want(t, c.post("/api/v1/webhooks/"+hookID+"/test", nil), http.StatusOK, "testing while the replica is behind")
	deliveryID := idOf(t, tested, "delivery")
	want(t, c.post("/api/v1/webhooks/"+hookID+"/deliveries/"+deliveryID+"/redeliver", nil), http.StatusOK, "redelivering while the replica is behind")
	if len(sink.got) != 2 {
		t.Fatalf("the endpoint received %d posts, want the test and the redelivery", len(sink.got))
	}
}
