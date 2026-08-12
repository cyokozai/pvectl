package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/Telmate/proxmox-api-go/proxmox"

	"github.com/cyokozai/pvectl/internal/config"
)

// telmateClient implements Client on top of the Telmate SDK. Config
// reads and writes go through the SDK's generic path-level helpers with
// flat Proxmox param maps; the SDK contributes session/auth handling,
// TLS, retries, and task (UPID) polling.
type telmateClient struct {
	c *proxmox.Client
}

func newTelmateClient(ctx context.Context, node config.Node, user config.User, opts Options) (*telmateClient, error) {
	apiURL := strings.TrimSuffix(node.Server, "/")
	if !strings.HasSuffix(apiURL, "/api2/json") {
		apiURL += "/api2/json"
	}

	timeout := opts.TaskTimeout
	if timeout <= 0 {
		timeout = DefaultTaskTimeout
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: node.InsecureSkipTLSVerify} //nolint:gosec // explicit opt-in via config

	client, err := proxmox.NewClient(apiURL, nil, "", tlsConfig, "", int(timeout.Seconds()), opts.Debug)
	if err != nil {
		return nil, fmt.Errorf("failed to create API client for %s: %w", node.Server, err)
	}

	switch {
	case user.Token != "":
		var token proxmox.ApiToken
		if err := token.Parse(user.Token); err != nil {
			return nil, fmt.Errorf("invalid API token (expected user@realm!tokenid=secret): %w", err)
		}
		client.SetAPIToken(token)
	case user.Username != "" || user.Password != "":
		if err := client.Login(ctx, user.Username, user.Password, ""); err != nil {
			return nil, fmt.Errorf("login failed for %s: %w", user.Username, err)
		}
	default:
		return nil, fmt.Errorf("no authentication method configured (set token or username/password)")
	}

	return &telmateClient{c: client}, nil
}

func (t *telmateClient) ListGuests(ctx context.Context) ([]GuestSummary, error) {
	list, err := t.c.GetResourceList(ctx, "vm")
	if err != nil {
		return nil, fmt.Errorf("failed to list guests: %w", err)
	}
	guests := make([]GuestSummary, 0, len(list))
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		guests = append(guests, GuestSummary{
			VMID:   int(asFloat(row["vmid"])),
			Name:   asString(row["name"]),
			Node:   asString(row["node"]),
			Type:   asString(row["type"]),
			Status: asString(row["status"]),
			Pool:   asString(row["pool"]),
			Tags:   splitTags(asString(row["tags"])),
			CPU:    asFloat(row["cpu"]),
			MaxCPU: int(asFloat(row["maxcpu"])),
			Mem:    uint64(asFloat(row["mem"])),
			MaxMem: uint64(asFloat(row["maxmem"])),
			Uptime: int64(asFloat(row["uptime"])),
		})
	}
	return guests, nil
}

func (t *telmateClient) FindGuest(ctx context.Context, name string) (*GuestRef, error) {
	guests, err := t.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	var matches []GuestSummary
	for _, g := range guests {
		if g.Name == name {
			matches = append(matches, g)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("guest %q: %w", name, ErrNotFound)
	case 1:
		return &GuestRef{VMID: matches[0].VMID, Node: matches[0].Node, Type: matches[0].Type}, nil
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = fmt.Sprintf("%d (node %s)", m.VMID, m.Node)
		}
		return nil, fmt.Errorf("guest %q matches vmids %s; identify it by spec.vmid: %w",
			name, strings.Join(ids, ", "), ErrAmbiguousName)
	}
}

func (t *telmateClient) GuestByID(ctx context.Context, vmid int) (*GuestRef, error) {
	guests, err := t.ListGuests(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range guests {
		if g.VMID == vmid {
			return &GuestRef{VMID: g.VMID, Node: g.Node, Type: g.Type}, nil
		}
	}
	return nil, fmt.Errorf("guest %d: %w", vmid, ErrNotFound)
}

func (t *telmateClient) QemuConfig(ctx context.Context, ref *GuestRef) (map[string]any, error) {
	cfg, err := t.c.GetItemConfigMapStringInterface(ctx,
		fmt.Sprintf("/nodes/%s/qemu/%d/config", ref.Node, ref.VMID), "vm", "CONFIG")
	if err != nil {
		return nil, fmt.Errorf("failed to read config of vm %d: %w", ref.VMID, err)
	}
	return cfg, nil
}

func (t *telmateClient) CreateQemu(ctx context.Context, node string, vmid int, params map[string]any) error {
	body := make(map[string]any, len(params)+1)
	for k, v := range params {
		body[k] = v
	}
	body["vmid"] = vmid
	if _, err := t.c.PostWithTask(ctx, body, fmt.Sprintf("/nodes/%s/qemu", node)); err != nil {
		return fmt.Errorf("failed to create vm %d on %s: %w", vmid, node, err)
	}
	return nil
}

func (t *telmateClient) UpdateQemuConfig(ctx context.Context, ref *GuestRef, params map[string]any) error {
	if _, err := t.c.PutWithTask(ctx, params, fmt.Sprintf("/nodes/%s/qemu/%d/config", ref.Node, ref.VMID)); err != nil {
		return fmt.Errorf("failed to update config of vm %d: %w", ref.VMID, err)
	}
	return nil
}

func (t *telmateClient) CloneQemu(ctx context.Context, src *GuestRef, params map[string]any) error {
	if _, err := t.c.PostWithTask(ctx, params, fmt.Sprintf("/nodes/%s/qemu/%d/clone", src.Node, src.VMID)); err != nil {
		return fmt.Errorf("failed to clone vm %d: %w", src.VMID, err)
	}
	return nil
}

func (t *telmateClient) DeleteGuest(ctx context.Context, ref *GuestRef) error {
	if _, err := t.c.DeleteWithTask(ctx, fmt.Sprintf("/nodes/%s/%s/%d", ref.Node, ref.Type, ref.VMID)); err != nil {
		return fmt.Errorf("failed to delete guest %d: %w", ref.VMID, err)
	}
	return nil
}

func (t *telmateClient) StartGuest(ctx context.Context, ref *GuestRef) error {
	if _, err := t.c.PostWithTask(ctx, nil, fmt.Sprintf("/nodes/%s/%s/%d/status/start", ref.Node, ref.Type, ref.VMID)); err != nil {
		return fmt.Errorf("failed to start guest %d: %w", ref.VMID, err)
	}
	return nil
}

func (t *telmateClient) StopGuest(ctx context.Context, ref *GuestRef) error {
	if _, err := t.c.PostWithTask(ctx, nil, fmt.Sprintf("/nodes/%s/%s/%d/status/stop", ref.Node, ref.Type, ref.VMID)); err != nil {
		return fmt.Errorf("failed to stop guest %d: %w", ref.VMID, err)
	}
	return nil
}

func (t *telmateClient) NextID(ctx context.Context) (int, error) {
	id, err := t.c.GetNextID(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get next vmid: %w", err)
	}
	return int(id), nil
}

func (t *telmateClient) Raw() RawClient { return rawClient{c: t.c} }

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ";")
}
