package events

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/armature/armature/backend/internal/observability"
)

// Continue begins the work a consumer group does for one event: a span named
// for the group and the topic, in the trace of the request that emitted it,
// and the counters for how it went. The returned done ends both. Every
// consumer says the same things in the same words.
func Continue(ctx context.Context, e Event, group string) (context.Context, func(err error)) {
	ctx = observability.Extract(ctx, e.Trace)
	ctx, end := observability.StartSpan(ctx, "events."+group+" "+e.Topic,
		attribute.String("armature.event_id", e.ID.String()),
		attribute.String("armature.topic", e.Topic),
		attribute.String("armature.group", group))
	started := time.Now()
	metrics := observability.Current()
	return ctx, func(err error) {
		end(err)
		metrics.Handled(group, e.Topic, err, time.Since(started))
	}
}
