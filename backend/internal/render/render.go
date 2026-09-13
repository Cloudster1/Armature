// Package render asks the render service, the browser suite's Chromium behind
// a small HTTP door, for a page of the product as a PDF over plain HTTP.
package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrUnavailable is returned when no render service is configured.
var ErrUnavailable = errors.New("PDF export is not set up on this deployment")

// Renderer turns a path of the web application into a PDF.
type Renderer interface {
	PDF(ctx context.Context, path string) (io.ReadCloser, error)
}

// DefaultTimeout is how long a render may take; it stays under the api's own
// route timeout so a slow page answers with a reason rather than a cut line.
const DefaultTimeout = 20 * time.Second

// HTTPRenderer is the render service at an address.
type HTTPRenderer struct {
	URL     string
	Timeout time.Duration
}

// PDF posts the path and streams the file back.
func (r HTTPRenderer) PDF(ctx context.Context, path string) (io.ReadCloser, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	body, _ := json.Marshal(map[string]string{"path": path})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL+"/pdf", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("render: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("render: the service answered %d", resp.StatusCode)
	}
	return &cancelling{ReadCloser: resp.Body, cancel: cancel}, nil
}

// cancelling ends the request's deadline when the file has been read.
type cancelling struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelling) Close() error {
	c.cancel()
	return c.ReadCloser.Close()
}

// Unavailable is the renderer of a deployment without one.
type Unavailable struct{}

// PDF says so.
func (Unavailable) PDF(context.Context, string) (io.ReadCloser, error) {
	return nil, ErrUnavailable
}
