package observability

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

// StreamCollector asks Redis, on each scrape, how many events each consumer
// group has been handed and not acknowledged: the number that says a consumer
// is stuck before anything downstream notices.
type StreamCollector struct {
	rdb    *redis.Client
	stream string
	groups []string
}

// NewStreamCollector watches the named groups on one stream.
func NewStreamCollector(rdb *redis.Client, stream string, groups []string) *StreamCollector {
	return &StreamCollector{rdb: rdb, stream: stream, groups: groups}
}

var streamPendingDesc = prometheus.NewDesc("armature_stream_pending", "Events delivered to a consumer group and not yet acknowledged.", []string{"group"}, nil)

// scrapeTimeout bounds one scrape's questions to Redis.
const scrapeTimeout = 2 * time.Second

func (c *StreamCollector) Describe(ch chan<- *prometheus.Desc) { ch <- streamPendingDesc }

func (c *StreamCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	for _, group := range c.groups {
		pending, err := c.rdb.XPending(ctx, c.stream, group).Result()
		if err != nil {
			// A group that does not exist yet has nothing pending.
			continue
		}
		ch <- prometheus.MustNewConstMetric(streamPendingDesc, prometheus.GaugeValue, float64(pending.Count), group)
	}
}
