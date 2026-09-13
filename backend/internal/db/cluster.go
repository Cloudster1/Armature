// Package db owns the Postgres connection topology: one writable primary and a
// rotation of read replicas, with routing that keeps a user's own writes visible
// to their subsequent reads.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/armature/armature/backend/internal/config"
)

// DBTX is the query surface shared by pgxpool.Pool and pgx.Tx. Repositories
// accept a DBTX so the same code runs against the primary or a replica, inside
// a transaction or not.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// replicaState tracks one read replica's health and replay position. All fields
// are read from request goroutines and written by the health loop, so every one
// of them is atomic.
type replicaState struct {
	name string
	pool *pgxpool.Pool

	healthy   atomic.Bool
	replayLSN atomic.Uint64
	lagMillis atomic.Int64
	lastErr   atomic.Pointer[string]
}

func (r *replicaState) snapshot() ReplicaStatus {
	var errStr string
	if p := r.lastErr.Load(); p != nil {
		errStr = *p
	}
	return ReplicaStatus{
		Name:      r.name,
		Healthy:   r.healthy.Load(),
		ReplayLSN: LSN(r.replayLSN.Load()),
		Lag:       time.Duration(r.lagMillis.Load()) * time.Millisecond,
		LastError: errStr,
	}
}

// ReplicaStatus is a point in time view of one replica, for health endpoints.
type ReplicaStatus struct {
	Name      string        `json:"name"`
	Healthy   bool          `json:"healthy"`
	ReplayLSN LSN           `json:"-"`
	Lag       time.Duration `json:"-"`
	LastError string        `json:"lastError,omitempty"`
}

// Cluster is the primary plus its replica rotation.
type Cluster struct {
	primary  *pgxpool.Pool
	admin    *pgxpool.Pool
	replicas []*replicaState

	maxLag  time.Duration
	rr      atomic.Uint64
	log     *slog.Logger
	stop    context.CancelFunc
	stopped chan struct{}

	// routing counters, exported through Stats for observability.
	readsToPrimary  atomic.Uint64
	readsToReplica  atomic.Uint64
	staleFallbacks  atomic.Uint64
	noHealthyReplic atomic.Uint64
}

// Option adjusts how the cluster is opened.
type Option func(*options)

type options struct {
	tracer func(pool string) pgx.QueryTracer
}

// WithQueryTracer gives every pool a tracer, made per pool so a span can say
// which one it ran on. The pools are named primary, admin and replica-<n>.
func WithQueryTracer(make func(pool string) pgx.QueryTracer) Option {
	return func(o *options) { o.tracer = make }
}

// Open connects the primary and every configured replica, then starts the
// health loop. The caller must Close the returned cluster.
func Open(ctx context.Context, cfg config.DB, log *slog.Logger, opts ...Option) (*Cluster, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	tracerFor := func(pool string) pgx.QueryTracer {
		if o.tracer == nil {
			return nil
		}
		return o.tracer(pool)
	}

	primary, err := openPool(ctx, cfg, cfg.PrimaryURL, tracerFor("primary"))
	if err != nil {
		return nil, fmt.Errorf("connect primary: %w", err)
	}

	admin := primary
	if cfg.AdminURL != "" && cfg.AdminURL != cfg.PrimaryURL {
		admin, err = openPool(ctx, cfg, cfg.AdminURL, tracerFor("admin"))
		if err != nil {
			primary.Close()
			return nil, fmt.Errorf("connect admin: %w", err)
		}
	}

	c := &Cluster{
		primary: primary,
		admin:   admin,
		maxLag:  cfg.MaxReplicaLag,
		log:     log,
		stopped: make(chan struct{}),
	}
	// Start the rotation at a random offset so that many api processes booting
	// at once do not all send their first read to the same replica.
	c.rr.Store(rand.Uint64())

	for i, url := range cfg.ReplicaURLs {
		name := fmt.Sprintf("replica-%d", i)
		pool, err := openPool(ctx, cfg, url, tracerFor(name))
		if err != nil {
			// A replica that is down at boot must not stop the process: the
			// health loop will pick it up when it returns.
			log.Warn("replica unavailable at startup", "replica", i, "error", err)
			continue
		}
		r := &replicaState{name: name, pool: pool}
		c.replicas = append(c.replicas, r)
	}

	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c.stop = cancel
	c.checkAll(loopCtx) // seed health before serving the first request
	go c.healthLoop(loopCtx, cfg.HealthInterval)

	log.Info("database cluster ready", "replicas", len(c.replicas), "max_lag", cfg.MaxReplicaLag)
	return c, nil
}

func openPool(ctx context.Context, cfg config.DB, url string, tracer pgx.QueryTracer) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = cfg.MinConns
	pcfg.MaxConnLifetime = cfg.ConnMaxLifetime
	if tracer != nil {
		pcfg.ConnConfig.Tracer = tracer
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Close shuts down the health loop and every pool.
func (c *Cluster) Close() {
	if c.stop != nil {
		c.stop()
		<-c.stopped
	}
	for _, r := range c.replicas {
		r.pool.Close()
	}
	if c.admin != c.primary {
		c.admin.Close()
	}
	c.primary.Close()
}

// Primary returns the writable pool. Prefer Write for anything that mutates.
func (c *Cluster) Primary() *pgxpool.Pool { return c.primary }

// reader picks the pool that should serve a read made with ctx.
//
// The rules, in order: an explicit primary pin wins; otherwise a replica must be
// healthy and, when the context carries a required LSN, must have replayed past
// it; if nothing qualifies the read goes to the primary. Falling back is always
// correct, only more expensive, so freshness never costs availability.
func (c *Cluster) reader(ctx context.Context) *pgxpool.Pool {
	r, outcome := pickReplica(c.replicas, int(c.rr.Add(1)), pinFrom(ctx))
	switch outcome {
	case routeReplica:
		c.readsToReplica.Add(1)
		return r.pool
	case routeStale:
		c.staleFallbacks.Add(1)
	case routeNoHealthy:
		c.noHealthyReplic.Add(1)
	}
	c.readsToPrimary.Add(1)
	return c.primary
}

// routeOutcome explains why a read was routed where it was.
type routeOutcome int

const (
	routeReplica   routeOutcome = iota // served by a replica
	routePinned                        // caller demanded the primary
	routeStale                         // replicas are up but none is caught up
	routeNoHealthy                     // no replica is currently usable
)

// pickReplica chooses the replica that should serve a read, or reports why none
// can. It is pure so that the routing rules can be tested without a database.
//
// The rules, in order: an explicit primary pin wins; otherwise a replica must be
// healthy and, when the caller carries a required LSN, must have replayed past
// it. Falling back to the primary is always correct, only more expensive, so
// freshness never costs availability.
func pickReplica(replicas []*replicaState, start int, p pin) (*replicaState, routeOutcome) {
	if p.forcePrimary {
		return nil, routePinned
	}
	n := len(replicas)
	if n == 0 {
		return nil, routeNoHealthy
	}
	if start < 0 {
		start = -start
	}
	sawHealthy := false
	for i := 0; i < n; i++ {
		r := replicas[(start+i)%n]
		if !r.healthy.Load() {
			continue
		}
		sawHealthy = true
		if p.requiredLSN != 0 && LSN(r.replayLSN.Load()) < p.requiredLSN {
			continue // has not caught up to the caller's own write
		}
		return r, routeReplica
	}
	if sawHealthy {
		return nil, routeStale
	}
	return nil, routeNoHealthy
}

func (c *Cluster) healthLoop(ctx context.Context, interval time.Duration) {
	defer close(c.stopped)
	if interval <= 0 {
		interval = 5 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.checkAll(ctx)
		}
	}
}

func (c *Cluster) checkAll(ctx context.Context) {
	for _, r := range c.replicas {
		c.check(ctx, r)
	}
}

// replicaHealthSQL reports whether the node is a standby, its replay position,
// how far behind it is, and whether its WAL receiver is actually connected.
//
// Two subtleties are worth spelling out, because getting either wrong makes the
// routing decisions nonsense:
//
// The CASE arms are load bearing. pg_current_wal_lsn errors on a standby and
// pg_last_wal_replay_lsn is null on a primary, so neither can be evaluated
// unconditionally.
//
// Lag is not simply now() minus the last replayed transaction timestamp. On an
// idle system that timestamp stops advancing, so a perfectly caught-up replica
// would report hours of "lag" and be dropped from the rotation for no reason.
// When everything received has been replayed the standby is by definition
// current, and lag is zero; the timestamp difference is only meaningful while
// there is a genuine backlog to work through.
const replicaHealthSQL = `
SELECT pg_is_in_recovery(),
       CASE WHEN pg_is_in_recovery()
            THEN COALESCE(pg_last_wal_replay_lsn(), '0/0'::pg_lsn)
            ELSE pg_current_wal_lsn()
       END::text,
       CASE
            WHEN NOT pg_is_in_recovery() THEN 0
            WHEN pg_last_wal_receive_lsn() IS NULL THEN 0
            WHEN pg_last_wal_receive_lsn() = pg_last_wal_replay_lsn() THEN 0
            ELSE GREATEST(0, EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp())))
       END,
       COALESCE((SELECT status FROM pg_stat_wal_receiver LIMIT 1), '')`

func (c *Cluster) check(ctx context.Context, r *replicaState) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var (
		inRecovery     bool
		lsnText        string
		lagSeconds     float64
		receiverStatus string
	)
	err := r.pool.QueryRow(ctx, replicaHealthSQL).Scan(&inRecovery, &lsnText, &lagSeconds, &receiverStatus)
	if err != nil {
		c.markUnhealthy(r, err.Error())
		return
	}
	lsn, err := ParseLSN(lsnText)
	if err != nil {
		c.markUnhealthy(r, err.Error())
		return
	}

	lag := time.Duration(lagSeconds * float64(time.Second))
	if lag < 0 {
		lag = 0 // clock skew between primary and standby
	}
	r.replayLSN.Store(uint64(lsn))
	r.lagMillis.Store(lag.Milliseconds())

	if inRecovery {
		// A standby whose receiver has dropped will happily report zero lag
		// forever, because it has replayed everything it received. Without this
		// check it would keep serving increasingly stale reads and look healthy
		// doing it. An empty status means the view is not readable by this
		// role, in which case we fall back to trusting the lag figure.
		if receiverStatus != "" && receiverStatus != "streaming" {
			c.markUnhealthy(r, fmt.Sprintf("wal receiver is %q, not streaming", receiverStatus))
			return
		}
		if c.maxLag > 0 && lag > c.maxLag {
			c.markUnhealthy(r, fmt.Sprintf("replication lag %s exceeds %s", lag, c.maxLag))
			return
		}
	}

	if was := r.healthy.Swap(true); !was {
		r.lastErr.Store(nil)
		c.log.Info("replica healthy", "replica", r.name, "lag", lag)
	}
}

func (c *Cluster) markUnhealthy(r *replicaState, reason string) {
	r.lastErr.Store(&reason)
	if was := r.healthy.Swap(false); was {
		c.log.Warn("replica unhealthy", "replica", r.name, "reason", reason)
	}
}

// Stats reports routing counters and replica health for the metrics endpoint.
type Stats struct {
	Replicas            []ReplicaStatus `json:"replicas"`
	ReadsToPrimary      uint64          `json:"readsToPrimary"`
	ReadsToReplica      uint64          `json:"readsToReplica"`
	StaleFallbacks      uint64          `json:"staleFallbacks"`
	NoHealthyReplicaHit uint64          `json:"noHealthyReplicaHits"`
	// Pools is how full each connection pool is, which is what a saturated
	// api looks like from the outside.
	Pools []PoolStats `json:"-"`
}

// PoolStats is a point in time count of one pool's connections.
type PoolStats struct {
	Name         string
	Idle         int32
	Acquired     int32
	Constructing int32
	Max          int32
}

func (c *Cluster) Stats() Stats {
	s := Stats{
		ReadsToPrimary:      c.readsToPrimary.Load(),
		ReadsToReplica:      c.readsToReplica.Load(),
		StaleFallbacks:      c.staleFallbacks.Load(),
		NoHealthyReplicaHit: c.noHealthyReplic.Load(),
	}
	type named struct {
		name string
		pool *pgxpool.Pool
	}
	pools := []named{{"primary", c.primary}}
	if c.admin != c.primary {
		pools = append(pools, named{"admin", c.admin})
	}
	for _, r := range c.replicas {
		s.Replicas = append(s.Replicas, r.snapshot())
		pools = append(pools, named{r.name, r.pool})
	}
	for _, p := range pools {
		stat := p.pool.Stat()
		s.Pools = append(s.Pools, PoolStats{Name: p.name, Idle: stat.IdleConns(), Acquired: stat.AcquiredConns(), Constructing: stat.ConstructingConns(), Max: stat.MaxConns()})
	}
	return s
}

// RefuseSuperuser fails when this connects as a role that ignores row level
// security, which would make every policy in the schema decoration.
func (c *Cluster) RefuseSuperuser(ctx context.Context) error {
	var privileged bool
	err := c.primary.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&privileged)
	if err != nil {
		return fmt.Errorf("ask which role this connects as: %w", err)
	}
	if privileged {
		return errors.New("this connects to the database as a role that bypasses row level security; use the application role")
	}
	return nil
}

// Ping verifies the primary is reachable.
func (c *Cluster) Ping(ctx context.Context) error {
	if err := c.primary.Ping(ctx); err != nil {
		return fmt.Errorf("primary: %w", err)
	}
	return nil
}
