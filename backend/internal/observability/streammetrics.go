package observability

import (
	"context"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

// StreamCollector asks Redis, on each scrape, how far behind each consumer
// group is: what it was handed and has not acknowledged, and what it has not
// been handed yet. Either one growing says a consumer is stuck.
type StreamCollector struct {
	rdb    *redis.Client
	stream string
	groups []string
}

// NewStreamCollector watches the named groups on one stream.
func NewStreamCollector(rdb *redis.Client, stream string, groups []string) *StreamCollector {
	return &StreamCollector{rdb: rdb, stream: stream, groups: groups}
}

var (
	streamPendingDesc = prometheus.NewDesc("armature_stream_pending", "Events delivered to a consumer group and not yet acknowledged.", []string{"group"}, nil)
	streamLagDesc     = prometheus.NewDesc("armature_stream_lag", "Events on the stream not yet delivered to a consumer group.", []string{"group"}, nil)
)

// scrapeTimeout bounds one scrape's questions to Redis.
const scrapeTimeout = 2 * time.Second

func (c *StreamCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- streamPendingDesc
	ch <- streamLagDesc
}

func (c *StreamCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	// No stream yet means no group has anything to be behind on.
	infos, err := c.rdb.XInfoGroups(ctx, c.stream).Result()
	if err != nil {
		return
	}
	for _, info := range infos {
		if !slices.Contains(c.groups, info.Name) {
			continue
		}
		ch <- prometheus.MustNewConstMetric(streamPendingDesc, prometheus.GaugeValue, float64(info.Pending), info.Name)
		// Redis cannot always count the lag, and a guess would page someone.
		if info.Lag >= 0 {
			ch <- prometheus.MustNewConstMetric(streamLagDesc, prometheus.GaugeValue, float64(info.Lag), info.Name)
		}
	}
}
