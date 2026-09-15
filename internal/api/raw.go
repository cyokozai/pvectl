package api

import (
	"context"
	"fmt"

	"github.com/Telmate/proxmox-api-go/proxmox"
)

// rawClient exposes path-level access on the shared SDK session for
// endpoints the typed Client interface does not model yet. Mutations
// still wait for task completion — there is no fire-and-forget path.
type rawClient struct {
	c *proxmox.Client
}

func (r rawClient) Get(ctx context.Context, path string) (map[string]any, error) {
	var data map[string]any
	if err := r.c.GetJsonRetryable(ctx, path, &data, 3); err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	return data, nil
}

func (r rawClient) PostTask(ctx context.Context, path string, params map[string]any) error {
	if _, err := r.c.PostWithTask(ctx, params, path); err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	return nil
}

func (r rawClient) PutTask(ctx context.Context, path string, params map[string]any) error {
	if _, err := r.c.PutWithTask(ctx, params, path); err != nil {
		return fmt.Errorf("PUT %s: %w", path, err)
	}
	return nil
}

func (r rawClient) DeleteTask(ctx context.Context, path string) error {
	if _, err := r.c.DeleteWithTask(ctx, path); err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	return nil
}
