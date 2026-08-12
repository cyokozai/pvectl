package resource

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cyokozai/pvectl/internal/runtime"
)

// Registry maps command-line names and manifest types to handlers.
type Registry struct {
	handlers []Handler
	byName   map[string]Handler
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: map[string]Handler{}}
}

// Register adds a handler under its kind and all its aliases.
func (r *Registry) Register(h Handler) {
	r.handlers = append(r.handlers, h)
	r.byName[strings.ToLower(h.Kind())] = h
	for _, alias := range h.Aliases() {
		r.byName[strings.ToLower(alias)] = h
	}
}

// Lookup resolves a kind or alias typed on the command line.
func (r *Registry) Lookup(kindOrAlias string) (Handler, error) {
	if h, ok := r.byName[strings.ToLower(kindOrAlias)]; ok {
		return h, nil
	}
	return nil, fmt.Errorf("unknown resource type %q (available: %s)",
		kindOrAlias, strings.Join(r.aliasSummary(), ", "))
}

// ForObject resolves the handler for a decoded manifest document.
func (r *Registry) ForObject(u *runtime.Unstructured) (Handler, error) {
	h, ok := r.byName[strings.ToLower(u.Kind)]
	if !ok {
		return nil, fmt.Errorf("%s: unknown kind %q (available: %s)",
			u.Source, u.Kind, strings.Join(r.Kinds(), ", "))
	}
	if u.APIVersion != h.APIVersion() {
		return nil, fmt.Errorf("%s: unsupported apiVersion %q for kind %s (expected %q)",
			u.Source, u.APIVersion, h.Kind(), h.APIVersion())
	}
	return h, nil
}

// Kinds returns all registered kinds, sorted.
func (r *Registry) Kinds() []string {
	kinds := make([]string, len(r.handlers))
	for i, h := range r.handlers {
		kinds[i] = h.Kind()
	}
	sort.Strings(kinds)
	return kinds
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
			names = append(names, strings.ToLower(h.Kind()))
		}
	}
	sort.Strings(names)
	return names
}
