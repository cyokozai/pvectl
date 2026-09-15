// Package apitest provides a stateful in-memory fake of api.Client for
// handler tests. It records every mutating call and keeps a guest list
// consistent enough for create-then-get test flows.
package apitest

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cyokozai/pvectl/internal/api"
)

// CreateCall records one CreateQemu invocation.
type CreateCall struct {
	Node   string
	VMID   int
	Params map[string]any
}

// UpdateCall records one UpdateQemuConfig invocation.
type UpdateCall struct {
	Ref    api.GuestRef
	Params map[string]any
}

// CloneCall records one CloneQemu invocation.
type CloneCall struct {
	Src    api.GuestRef
	Params map[string]any
}

// RawCall records one RawClient invocation.
type RawCall struct {
	Method string
	Path   string
	Params map[string]any
}

// Fake implements api.Client in memory.
type Fake struct {
	GuestList   []api.GuestSummary
	Configs     map[int]map[string]any
	NextIDValue int

	// Err injects an error per method name (e.g. "CreateQemu").
	Err map[string]error

	Creates []CreateCall
	Updates []UpdateCall
	Clones  []CloneCall
	Deletes []api.GuestRef
	Starts  []api.GuestRef
	Stops   []api.GuestRef
	Raws    []RawCall
}

var _ api.Client = (*Fake)(nil)

// AddGuest seeds a guest and its config.
func (f *Fake) AddGuest(g api.GuestSummary, config map[string]any) {
	if g.Type == "" {
		g.Type = "qemu"
	}
	if g.Status == "" {
		g.Status = "stopped"
	}
	f.GuestList = append(f.GuestList, g)
	if f.Configs == nil {
		f.Configs = map[int]map[string]any{}
	}
	if config == nil {
		config = map[string]any{}
	}
	f.Configs[g.VMID] = config
}

func (f *Fake) fail(method string) error {
	if f.Err == nil {
		return nil
	}
	return f.Err[method]
}

func (f *Fake) ListGuests(context.Context) ([]api.GuestSummary, error) {
	if err := f.fail("ListGuests"); err != nil {
		return nil, err
	}
	return append([]api.GuestSummary(nil), f.GuestList...), nil
}

func (f *Fake) FindGuest(ctx context.Context, name string) (*api.GuestRef, error) {
	if err := f.fail("FindGuest"); err != nil {
		return nil, err
	}
	var matches []api.GuestSummary
	for _, g := range f.GuestList {
		if g.Name == name {
			matches = append(matches, g)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("guest %q: %w", name, api.ErrNotFound)
	case 1:
		return &api.GuestRef{VMID: matches[0].VMID, Node: matches[0].Node, Type: matches[0].Type}, nil
	default:
		return nil, fmt.Errorf("guest %q matches several vmids: %w", name, api.ErrAmbiguousName)
	}
}

func (f *Fake) GuestByID(ctx context.Context, vmid int) (*api.GuestRef, error) {
	if err := f.fail("GuestByID"); err != nil {
		return nil, err
	}
	for _, g := range f.GuestList {
		if g.VMID == vmid {
			return &api.GuestRef{VMID: g.VMID, Node: g.Node, Type: g.Type}, nil
		}
	}
	return nil, fmt.Errorf("guest %d: %w", vmid, api.ErrNotFound)
}

func (f *Fake) QemuConfig(ctx context.Context, ref *api.GuestRef) (map[string]any, error) {
	if err := f.fail("QemuConfig"); err != nil {
		return nil, err
	}
	cfg, ok := f.Configs[ref.VMID]
	if !ok {
		return nil, fmt.Errorf("guest %d: %w", ref.VMID, api.ErrNotFound)
	}
	copied := map[string]any{}
	for k, v := range cfg {
		copied[k] = v
	}
	return copied, nil
}

func (f *Fake) CreateQemu(ctx context.Context, node string, vmid int, params map[string]any) error {
	if err := f.fail("CreateQemu"); err != nil {
		return err
	}
	f.Creates = append(f.Creates, CreateCall{Node: node, VMID: vmid, Params: params})
	name, _ := params["name"].(string)
	f.AddGuest(api.GuestSummary{VMID: vmid, Name: name, Node: node, Type: "qemu"}, stringify(params))
	return nil
}

func (f *Fake) UpdateQemuConfig(ctx context.Context, ref *api.GuestRef, params map[string]any) error {
	if err := f.fail("UpdateQemuConfig"); err != nil {
		return err
	}
	f.Updates = append(f.Updates, UpdateCall{Ref: *ref, Params: params})
	cfg := f.Configs[ref.VMID]
	if cfg == nil {
		cfg = map[string]any{}
		if f.Configs == nil {
			f.Configs = map[int]map[string]any{}
		}
		f.Configs[ref.VMID] = cfg
	}
	for k, v := range stringify(params) {
		cfg[k] = v
	}
	if name, ok := params["name"].(string); ok {
		for i := range f.GuestList {
			if f.GuestList[i].VMID == ref.VMID {
				f.GuestList[i].Name = name
			}
		}
	}
	return nil
}

func (f *Fake) CloneQemu(ctx context.Context, src *api.GuestRef, params map[string]any) error {
	if err := f.fail("CloneQemu"); err != nil {
		return err
	}
	f.Clones = append(f.Clones, CloneCall{Src: *src, Params: params})

	newID, _ := params["newid"].(int)
	name, _ := params["name"].(string)
	target, _ := params["target"].(string)
	if target == "" {
		target = src.Node
	}
	config := map[string]any{}
	for k, v := range f.Configs[src.VMID] {
		config[k] = v
	}
	config["name"] = name
	f.AddGuest(api.GuestSummary{VMID: newID, Name: name, Node: target, Type: src.Type}, config)
	return nil
}

func (f *Fake) DeleteGuest(ctx context.Context, ref *api.GuestRef) error {
	if err := f.fail("DeleteGuest"); err != nil {
		return err
	}
	f.Deletes = append(f.Deletes, *ref)
	kept := f.GuestList[:0]
	for _, g := range f.GuestList {
		if g.VMID != ref.VMID {
			kept = append(kept, g)
		}
	}
	f.GuestList = kept
	delete(f.Configs, ref.VMID)
	return nil
}

func (f *Fake) StartGuest(ctx context.Context, ref *api.GuestRef) error {
	if err := f.fail("StartGuest"); err != nil {
		return err
	}
	f.Starts = append(f.Starts, *ref)
	f.setStatus(ref.VMID, "running")
	return nil
}

func (f *Fake) StopGuest(ctx context.Context, ref *api.GuestRef) error {
	if err := f.fail("StopGuest"); err != nil {
		return err
	}
	f.Stops = append(f.Stops, *ref)
	f.setStatus(ref.VMID, "stopped")
	return nil
}

func (f *Fake) NextID(context.Context) (int, error) {
	if err := f.fail("NextID"); err != nil {
		return 0, err
	}
	if f.NextIDValue == 0 {
		f.NextIDValue = 100
	}
	id := f.NextIDValue
	f.NextIDValue++
	return id, nil
}

func (f *Fake) Raw() api.RawClient { return fakeRaw{f} }

func (f *Fake) setStatus(vmid int, status string) {
	for i := range f.GuestList {
		if f.GuestList[i].VMID == vmid {
			f.GuestList[i].Status = status
		}
	}
}

// stringify converts params the way the real API echoes them back:
// every value becomes its string form.
func stringify(params map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range params {
		switch x := v.(type) {
		case string:
			out[k] = x
		case int:
			out[k] = strconv.Itoa(x)
		default:
			out[k] = fmt.Sprintf("%v", v)
		}
	}
	return out
}

type fakeRaw struct{ f *Fake }

func (r fakeRaw) Get(ctx context.Context, path string) (map[string]any, error) {
	r.f.Raws = append(r.f.Raws, RawCall{Method: "GET", Path: path})
	return map[string]any{}, nil
}

func (r fakeRaw) PostTask(ctx context.Context, path string, params map[string]any) error {
	r.f.Raws = append(r.f.Raws, RawCall{Method: "POST", Path: path, Params: params})
	return nil
}

func (r fakeRaw) PutTask(ctx context.Context, path string, params map[string]any) error {
	r.f.Raws = append(r.f.Raws, RawCall{Method: "PUT", Path: path, Params: params})
	return nil
}

func (r fakeRaw) DeleteTask(ctx context.Context, path string) error {
	r.f.Raws = append(r.f.Raws, RawCall{Method: "DELETE", Path: path})
	return nil
}
