// Package attachment owns the files on an issue: the rows the tracker keeps
// about them and the object store the bytes live in.
package attachment

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

// Store is where the bytes go. The service knows nothing about S3 beyond this,
// so a test can hold the bytes in memory and a deployment can point at any
// bucket that speaks the S3 protocol.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// ErrUnavailable is returned when no object store is configured. It is a
// deployment problem, not a user's, and the message says which setting fixes it.
var ErrUnavailable = errors.New("attachments are not configured: set ARMATURE_S3_ENDPOINT to an S3 compatible store")

// ErrNoObject is returned when the row exists but the bytes are gone.
var ErrNoObject = errors.New("the file behind this attachment is missing from storage")

// S3Config is what it takes to reach a bucket.
type S3Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

// Configured reports whether a store was asked for at all.
func (c S3Config) Configured() bool { return c.Endpoint != "" }

// MemoryStore holds objects in a map. It is for tests, and for nothing else.
type MemoryStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{objects: map[string][]byte{}} }

func (m *MemoryStore) Put(_ context.Context, key string, body io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return nil
}

func (m *MemoryStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, ErrNoObject
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *MemoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

// Len is how many objects are held, for a test to count.
func (m *MemoryStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}

// Unavailable is the store a deployment gets when none was configured. Every
// call fails the same way, so an upload is refused with the setting to fix.
type Unavailable struct{}

func (Unavailable) Put(context.Context, string, io.Reader, int64, string) error {
	return ErrUnavailable
}
func (Unavailable) Get(context.Context, string) (io.ReadCloser, error) { return nil, ErrUnavailable }
func (Unavailable) Delete(context.Context, string) error               { return ErrUnavailable }

// FromConfig picks the store a configuration asks for. A missing endpoint is a
// store that refuses, not a store that pretends.
func FromConfig(cfg S3Config) (Store, error) {
	if !cfg.Configured() {
		return Unavailable{}, nil
	}
	return NewS3(cfg)
}
