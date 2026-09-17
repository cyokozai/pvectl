package api

import (
	"context"
	"fmt"

	"github.com/Telmate/proxmox-api-go/proxmox"

	"github.com/cyokozai/pvectl/internal/verbose"
)

// rawClient exposes path-level access on the shared SDK session for
// endpoints the typed Client interface does not model yet. Mutations
// still wait for task completion — there is no fire-and-forget path.
type rawClient struct {
	c   *proxmox.Client
	log *verbose.Logger
}

// Get reads a path, letting the SDK retry a transient failure a few
// times. The retry is worth having for one-shot reads; callers that
// poll in a loop of their own want getOnce instead.
func (r rawClient) Get(ctx context.Context, path string) (map[string]any, error) {
	return r.get(ctx, path, 3)
}

// getOnce reads a path with a single attempt. A caller that already
// polls does not need the SDK to poll underneath it, and the SDK's
// retry is what used to break the caller's deadline (see get).
func (r rawClient) getOnce(ctx context.Context, path string) (map[string]any, error) {
	return r.get(ctx, path, 1)
}

// get performs the SDK's JSON GET, giving up on it the moment ctx is
// done.
//
// proxmox.Client.GetJsonRetryable sleeps (attempt+1) seconds between
// attempts and never consults ctx, so three tries can hold a caller
// six seconds past a deadline it set itself — and a request that fails
// *because* the deadline expired is exactly the kind the SDK counts as
// retryable. Even tries=1 sleeps once before giving up. Bounding the
// call with ctx puts the deadline back where the caller put it.
func (r rawClient) get(ctx context.Context, path string, tries int) (map[string]any, error) {
	data, err := withinCtx(ctx, func() (map[string]any, error) {
		var data map[string]any
		err := r.c.GetJsonRetryable(ctx, path, &data, tries)
		return data, err
	})
	switch {
	case err == nil:
		return data, nil
	case ctx.Err() != nil:
		// The deadline, not the endpoint, is why this failed; say so
		// plainly so callers can recognise it with errors.Is.
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
}

// withinCtx runs call on its own goroutine and returns as soon as ctx
// is done, whether or not call has noticed. It is the wrapper for SDK
// entry points that sleep without watching the context.
//
// The abandoned goroutine finishes its sleep and exits on its own; the
// channel is buffered so its send never blocks on a receiver that has
// already left, which is what would turn the abandonment into a leak.
func withinCtx[T any](ctx context.Context, call func() (T, error)) (T, error) {
	type outcome struct {
		value T
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		value, err := call()
		done <- outcome{value: value, err: err}
	}()

	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case res := <-done:
		return res.value, res.err
	}
}

func (r rawClient) PostTask(ctx context.Context, path string, params map[string]any) error {
	done := logTask(r.log, "POST "+path, params)
	_, err := r.c.PostWithTask(ctx, params, path)
	done(err)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	return nil
}

func (r rawClient) PutTask(ctx context.Context, path string, params map[string]any) error {
	done := logTask(r.log, "PUT "+path, params)
	_, err := r.c.PutWithTask(ctx, params, path)
	done(err)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", path, err)
	}
	return nil
}

func (r rawClient) DeleteTask(ctx context.Context, path string) error {
	done := logTask(r.log, "DELETE "+path, nil)
	_, err := r.c.DeleteWithTask(ctx, path)
	done(err)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	return nil
}
