package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/Telmate/proxmox-api-go/proxmox"

	"github.com/cyokozai/pvectl/internal/config"
	"github.com/cyokozai/pvectl/internal/verbose"
)

// telmateClient implements Client on top of the Telmate SDK. Config
// reads and writes go through the SDK's generic path-level helpers with
// flat Proxmox param maps; the SDK contributes session/auth handling,
// TLS, retries, and task (UPID) polling.
type telmateClient struct {
	c   *proxmox.Client
	log *verbose.Logger
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

	log := opts.Logger
	// The credentials go to the logger before the first request, so no
	// line can be written while they are still unknown to its sweep.
	log.Secret(tokenSecrets(user.Token)...)
	log.Secret(user.Password)

	// The SDK's own debug flag stays off at every -v level: it dumps the
	// Authorization header verbatim (see loggingTransport). The custom
	// http.Client is nil below -v=6, so the default path is exactly the
	// one the SDK builds for itself.
	const sdkDebug = false
	client, err := proxmox.NewClient(apiURL, newHTTPClient(tlsConfig, log), "", tlsConfig, "", int(timeout.Seconds()), sdkDebug)
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
		log.Logf(verbose.LevelContext, "authenticating to %s with an API token", apiURL)
	case user.Username != "" || user.Password != "":
		log.Logf(verbose.LevelContext, "authenticating to %s as %s with a password (ticket login)", apiURL, user.Username)
		if err := client.Login(ctx, user.Username, user.Password, ""); err != nil {
			return nil, fmt.Errorf("login failed for %s: %w", user.Username, err)
		}
	default:
		return nil, fmt.Errorf("no authentication method configured (set token or username/password)")
	}

	return &telmateClient{c: client, log: log}, nil
}

// task brackets a task-bearing call; see logTask.
func (t *telmateClient) task(what string, params map[string]any) func(error) {
	return logTask(t.log, what, params)
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
		t.log.Logf(verbose.LevelResolve, "no guest named %q among %d cluster guests", name, len(guests))
		return nil, fmt.Errorf("guest %q: %w", name, ErrNotFound)
	case 1:
		t.log.Logf(verbose.LevelResolve, "resolved guest %q to vmid %d on node %s (%s)",
			name, matches[0].VMID, matches[0].Node, matches[0].Type)
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
			t.log.Logf(verbose.LevelResolve, "resolved vmid %d to node %s (%s, name %q)", g.VMID, g.Node, g.Type, g.Name)
			return &GuestRef{VMID: g.VMID, Node: g.Node, Type: g.Type}, nil
		}
	}
	t.log.Logf(verbose.LevelResolve, "no guest with vmid %d among %d cluster guests", vmid, len(guests))
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
	path := fmt.Sprintf("/nodes/%s/qemu", node)
	done := t.task("POST "+path, body)
	_, err := t.c.PostWithTask(ctx, body, path)
	done(err)
	if err != nil {
		return fmt.Errorf("failed to create vm %d on %s: %w", vmid, node, err)
	}
	return nil
}

func (t *telmateClient) UpdateQemuConfig(ctx context.Context, ref *GuestRef, params map[string]any) error {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/config", ref.Node, ref.VMID)
	done := t.task("PUT "+path, params)
	_, err := t.c.PutWithTask(ctx, params, path)
	done(err)
	if err != nil {
		return fmt.Errorf("failed to update config of vm %d: %w", ref.VMID, err)
	}
	return nil
}

func (t *telmateClient) CloneQemu(ctx context.Context, src *GuestRef, params map[string]any) error {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/clone", src.Node, src.VMID)
	done := t.task("POST "+path, params)
	_, err := t.c.PostWithTask(ctx, params, path)
	done(err)
	if err != nil {
		return fmt.Errorf("failed to clone vm %d: %w", src.VMID, err)
	}
	return nil
}

func (t *telmateClient) DeleteGuest(ctx context.Context, ref *GuestRef) error {
	path := fmt.Sprintf("/nodes/%s/%s/%d", ref.Node, ref.Type, ref.VMID)
	done := t.task("DELETE "+path, nil)
	_, err := t.c.DeleteWithTask(ctx, path)
	done(err)
	if err != nil {
		return fmt.Errorf("failed to delete guest %d: %w", ref.VMID, err)
	}
	return nil
}

func (t *telmateClient) StartGuest(ctx context.Context, ref *GuestRef) error {
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/start", ref.Node, ref.Type, ref.VMID)
	done := t.task("POST "+path, nil)
	_, err := t.c.PostWithTask(ctx, nil, path)
	done(err)
	if err != nil {
		return fmt.Errorf("failed to start guest %d: %w", ref.VMID, err)
	}
	return nil
}

func (t *telmateClient) StopGuest(ctx context.Context, ref *GuestRef) error {
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/stop", ref.Node, ref.Type, ref.VMID)
	done := t.task("POST "+path, nil)
	_, err := t.c.PostWithTask(ctx, nil, path)
	done(err)
	if err != nil {
		return fmt.Errorf("failed to stop guest %d: %w", ref.VMID, err)
	}
	return nil
}

func (t *telmateClient) MigrateGuest(ctx context.Context, ref *GuestRef, target string, online bool) error {
	params := map[string]any{"target": target}
	if online {
		params["online"] = true
	}
	path := fmt.Sprintf("/nodes/%s/%s/%d/migrate", ref.Node, guestType(ref), ref.VMID)
	done := t.task("POST "+path, params)
	_, err := t.c.PostWithTask(ctx, params, path)
	done(err)
	if err != nil {
		return fmt.Errorf("failed to migrate guest %d from %s to %s: %w", ref.VMID, ref.Node, target, err)
	}
	return nil
}

func (t *telmateClient) GuestExec(ctx context.Context, ref *GuestRef, command []string) (int, error) {
	vmr := proxmox.NewVmRef(proxmox.GuestID(ref.VMID)) //nolint:gosec // vmids are small positive integers
	vmr.SetNode(ref.Node)
	vmr.SetVmType(proxmox.GuestQemu)

	// []string is form-encoded as a repeated "command" key, which is how
	// the API takes "program plus arguments" since PVE 7.
	//
	// The POST itself is a single request, but QemuAgentExec resolves an
	// incomplete VmRef first, and that resolution goes through the SDK's
	// retrying GET. Bounding the whole call keeps a ref without a node
	// from spending the caller's deadline on retry sleeps.
	result, err := withinCtx(ctx, func() (map[string]any, error) {
		return t.c.QemuAgentExec(ctx, vmr, map[string]any{"command": command})
	})
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, agentError(ref, "agent exec", err)
	}
	pid, ok := result["pid"]
	if !ok {
		return 0, fmt.Errorf("agent exec on vm %d: response carried no pid", ref.VMID)
	}
	return asInt(pid), nil
}

func (t *telmateClient) GuestExecStatus(ctx context.Context, ref *GuestRef, pid int) (*ExecStatus, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec-status?pid=%d", ref.Node, ref.VMID, pid)
	// getOnce, not Get: ExecWait already polls, so a retry underneath it
	// buys nothing and its sleeps would outlast --exec-timeout.
	body, err := t.raw().getOnce(ctx, path)
	if err != nil {
		return nil, agentError(ref, "agent exec-status", err)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("agent exec-status of vm %d: unexpected response %v", ref.VMID, body)
	}
	return parseExecStatus(data), nil
}

func (t *telmateClient) NextID(ctx context.Context) (int, error) {
	id, err := t.c.GetNextID(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get next vmid: %w", err)
	}
	t.log.Logf(verbose.LevelResolve, "cluster reported next free vmid %d", int(id))
	return int(id), nil
}

func (t *telmateClient) Raw() RawClient { return t.raw() }

// raw is Raw without the interface, for the paths inside this package
// that need rawClient's unexported helpers.
func (t *telmateClient) raw() rawClient { return rawClient{c: t.c, log: t.log} }

// guestType defaults an unset ref type to qemu so callers that built a
// ref by hand still produce a valid path.
func guestType(ref *GuestRef) string {
	if ref.Type == "" {
		return "qemu"
	}
	return ref.Type
}

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
