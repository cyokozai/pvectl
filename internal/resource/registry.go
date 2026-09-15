package resource

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cyokozai/pvectl/internal/runtime"
)

// Registry maps command-line names and manifest types to handlers.
// Manifests resolve by full GVK; command-line names (kinds and aliases)
// resolve through a separate short-name index, which may map one name
// to several GVKs once kinds live in different API groups (ADR-006).
type Registry struct {
	handlers []Handler
	byGVK    map[runtime.GVK]Handler
	byName   map[string][]runtime.GVK
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		byGVK:  map[runtime.GVK]Handler{},
		byName: map[string][]runtime.GVK{},
	}
}

// Register adds a handler under its GVK, and indexes its kind and all
// its aliases as command-line short names.
func (r *Registry) Register(h Handler) {
	gvk := h.GVK()
	r.handlers = append(r.handlers, h)
	r.byGVK[gvk] = h
	r.addName(gvk.Kind, gvk)
	for _, alias := range h.Aliases() {
		r.addName(alias, gvk)
	}
}

// addName indexes one short name, keeping the mapping free of duplicates
// so the same handler registered twice stays unambiguous.
func (r *Registry) addName(name string, gvk runtime.GVK) {
	key := strings.ToLower(name)
	for _, existing := range r.byName[key] {
		if existing == gvk {
			return
		}
	}
	r.byName[key] = append(r.byName[key], gvk)
}

// Lookup resolves a kind or alias typed on the command line. A short
// name shared by several GVKs is ambiguous and must be disambiguated by
// the caller; it is reported as an error listing the candidates.
func (r *Registry) Lookup(kindOrAlias string) (Handler, error) {
	gvks := r.byName[strings.ToLower(kindOrAlias)]
	switch len(gvks) {
	case 0:
		return nil, fmt.Errorf("unknown resource type %q (available: %s)",
			kindOrAlias, strings.Join(r.aliasSummary(), ", "))
	case 1:
		return r.byGVK[gvks[0]], nil
	default:
		return nil, fmt.Errorf("ambiguous resource type %q (matches: %s)",
			kindOrAlias, strings.Join(gvkStrings(gvks), ", "))
	}
}

// ForObject resolves the handler for a decoded manifest document by
// exact group/version/kind match.
func (r *Registry) ForObject(u *runtime.Unstructured) (Handler, error) {
	gvk, err := u.GVK()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u.Source, err)
	}
	if h, ok := r.byGVK[gvk]; ok {
		return h, nil
	}
	if known := r.gvksForKind(gvk.Kind); len(known) > 0 {
		return nil, fmt.Errorf("%s: unsupported apiVersion %q for kind %s (expected %s)",
			u.Source, u.APIVersion, gvk.Kind, strings.Join(quoted(apiVersions(known)), ", "))
	}
	return nil, fmt.Errorf("%s: unknown kind %q (available: %s)",
		u.Source, u.Kind, strings.Join(r.Kinds(), ", "))
}

// Kinds returns all registered kinds, sorted.
func (r *Registry) Kinds() []string {
	kinds := make([]string, len(r.handlers))
	for i, h := range r.handlers {
		kinds[i] = h.GVK().Kind
	}
	sort.Strings(kinds)
	return kinds
}

// gvksForKind returns every registered GVK carrying the given kind,
// sorted by apiVersion.
func (r *Registry) gvksForKind(kind string) []runtime.GVK {
	var gvks []runtime.GVK
	for _, h := range r.handlers {
		if gvk := h.GVK(); gvk.Kind == kind {
			gvks = append(gvks, gvk)
		}
	}
	sort.Slice(gvks, func(i, j int) bool { return gvks[i].APIVersion() < gvks[j].APIVersion() })
	return gvks
}

// aliasSummary lists each kind's shortest alias plus the kind itself,
// for "unknown resource type" error messages.
func (r *Registry) aliasSummary() []string {
	var names []string
	for _, h := range r.handlers {
		aliases := h.Aliases()
		if len(aliases) > 0 {
			names = append(names, aliases[0])
		} else {
			names = append(names, strings.ToLower(h.GVK().Kind))
		}
	}
	sort.Strings(names)
	return names
}

// gvkStrings renders GVKs for error messages, sorted for stable output.
func gvkStrings(gvks []runtime.GVK) []string {
	out := make([]string, len(gvks))
	for i, gvk := range gvks {
		out[i] = gvk.String()
	}
	sort.Strings(out)
	return out
}

// apiVersions renders the apiVersion of each GVK.
func apiVersions(gvks []runtime.GVK) []string {
	out := make([]string, len(gvks))
	for i, gvk := range gvks {
		out[i] = gvk.APIVersion()
	}
	return out
}

// quoted wraps each string in Go-style quotes.
func quoted(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}
