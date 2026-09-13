// Package events implements the transactional outbox.
//
// A domain event is written in the same transaction as the change that caused
// it. That is the whole point: "the issue moved to Done" and "tell the watchers"
// can never disagree, because either both are committed or neither is. A relay
// then moves committed events onto Redis for the worker to consume, at least
// once.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
)

// Topics are dotted names, coarse enough to subscribe to and specific enough to
// route on.
const (
	TopicOrgCreated          = "org.created"
	TopicMemberJoined        = "member.joined"
	TopicIssueCreated        = "issue.created"
	TopicIssueUpdated        = "issue.updated"
	TopicIssueTransitioned   = "issue.transitioned"
	TopicCommentAdded        = "comment.added"
	TopicPullRequestLinked   = "vcs.pull_request.linked"
	TopicCommitLinked        = "vcs.commit.linked"
	TopicCIRunRecorded       = "ci.run.recorded"
	TopicDeploymentSucceeded = "ci.deployment.succeeded"
	TopicSLABreached         = "sla.breached"
	TopicAttachmentAdded     = "attachment.added"
	TopicWorkLogged          = "worklog.added"
	TopicWatcherAdded        = "watcher.added"
	// The automation's own topics: a schedule that came due, a call on an
	// incoming hook, a rule run by hand.
	TopicAutomationScheduled = "automation.scheduled"
	TopicAutomationIncoming  = "automation.incoming"
	TopicAutomationManual    = "automation.manual"
)

// Topics lists every topic, in the order a subscription picker shows them.
var Topics = []string{
	TopicOrgCreated, TopicMemberJoined, TopicIssueCreated, TopicIssueUpdated, TopicIssueTransitioned,
	TopicCommentAdded, TopicPullRequestLinked, TopicCommitLinked, TopicCIRunRecorded, TopicDeploymentSucceeded,
	TopicSLABreached, TopicAttachmentAdded, TopicWorkLogged, TopicWatcherAdded,
	TopicAutomationScheduled, TopicAutomationIncoming, TopicAutomationManual,
}

// Known says whether a topic is one the product sends.
func Known(topic string) bool {
	for _, t := range Topics {
		if t == topic {
			return true
		}
	}
	return false
}

// Groups names every consumer group the worker runs on the stream, so a
// collector can ask after each without knowing the packages.
var Groups = []string{"audit", "automation", "desk-mail", "git-sync", "notify", "webhooks"}

// Event is one committed domain event.
type Event struct {
	ID        uuid.UUID       `json:"id"`
	OrgID     uuid.UUID       `json:"orgId"`
	Topic     string          `json:"topic"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
	// Trace is the W3C traceparent of the request that emitted the event, so
	// what the worker does for it joins that trace. Empty when none was on.
	Trace string `json:"trace,omitempty"`
}

// Emit writes an event inside the caller's transaction. It must be called with
// the same DBTX as the change it describes, never afterwards on a fresh one.
func Emit(ctx context.Context, tx db.DBTX, orgID uuid.UUID, topic string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", topic, err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_event (org_id, topic, payload, trace_parent)
		VALUES ($1, $2, $3, nullif($4, ''))`,
		orgID, topic, body, observability.Inject(ctx))
	if err != nil {
		return fmt.Errorf("emit %s: %w", topic, err)
	}
	return nil
}

// EmitInTenant writes an event for the transaction's current organization,
// which saves the caller threading the id through.
func EmitInTenant(ctx context.Context, tx db.DBTX, topic string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", topic, err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_event (org_id, topic, payload, trace_parent)
		VALUES (current_org_id(), $1, $2, nullif($3, ''))`,
		topic, body, observability.Inject(ctx))
	if err != nil {
		return fmt.Errorf("emit %s: %w", topic, err)
	}
	return nil
}
