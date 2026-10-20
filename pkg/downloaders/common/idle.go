package common

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SetIdleTimeout makes timeout a stall limit for c instead of a cap on the
// whole request. http.Client.Timeout also covers reading the body, so any
// file that takes longer than --timeout to transfer was killed midway. Here a
// request is abandoned only when no bytes arrive for timeout (while waiting
// for headers or between body reads).
func SetIdleTimeout(c *http.Client, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.Timeout = 0
	c.Transport = &idleTransport{base: base, idle: timeout}
}

type idleTransport struct {
	base http.RoundTripper
	idle time.Duration
}

func (t *idleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	timer := time.AfterFunc(t.idle, func() {
		cancel(fmt.Errorf("no data received for %s (--timeout)", t.idle))
	})

	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		timer.Stop()
		if cause := context.Cause(ctx); cause != nil && req.Context().Err() == nil {
			err = cause
		}
		cancel(nil)
		return nil, err
	}

	resp.Body = &idleBody{ReadCloser: resp.Body, ctx: ctx, cancel: cancel, timer: timer, idle: t.idle}
	return resp, nil
}

type idleBody struct {
	io.ReadCloser
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  *time.Timer
	idle   time.Duration
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.timer.Reset(b.idle)
	}
	if err != nil && err != io.EOF {
		if cause := context.Cause(b.ctx); cause != nil {
			err = cause
		}
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	b.cancel(nil)
	return b.ReadCloser.Close()
}
