package db

import "context"

type pinKey struct{}

// pin describes a freshness requirement placed on reads made with a context.
type pin struct {
	// forcePrimary sends every read to the primary regardless of replica state.
	forcePrimary bool
	// requiredLSN is the write ahead log position a replica must have replayed
	// before it may serve this context's reads.
	requiredLSN LSN
}

// PinPrimary forces every read made with the returned context to the primary.
// Use it for the rare read that must observe the absolute latest state, such as
// re-reading a row inside a compare-and-set loop.
func PinPrimary(ctx context.Context) context.Context {
	p, _ := ctx.Value(pinKey{}).(pin)
	p.forcePrimary = true
	return context.WithValue(ctx, pinKey{}, p)
}

// PinLSN requires that any replica serving this context's reads has replayed at
// least lsn. This is the read-your-writes mechanism: after a write we remember
// the resulting LSN for the user and pin their subsequent reads to it, so they
// never see a stale replica hide the change they just made. If no replica has
// caught up, the read falls back to the primary rather than blocking.
func PinLSN(ctx context.Context, lsn LSN) context.Context {
	p, _ := ctx.Value(pinKey{}).(pin)
	if lsn > p.requiredLSN {
		p.requiredLSN = lsn
	}
	return context.WithValue(ctx, pinKey{}, p)
}

// pinFrom returns the freshness requirement attached to ctx.
func pinFrom(ctx context.Context) pin {
	p, _ := ctx.Value(pinKey{}).(pin)
	return p
}
