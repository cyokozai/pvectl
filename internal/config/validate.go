package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Validate checks the config for structural and referential integrity.
// All problems are reported at once, one per line.
func (c *Config) Validate() error {
	var errs []string

	if c.APIVersion != "v1" {
		errs = append(errs, fmt.Sprintf("unsupported apiVersion %q (expected \"v1\")", c.APIVersion))
	}
	if c.Kind != "Config" {
		errs = append(errs, fmt.Sprintf("unsupported kind %q (expected \"Config\")", c.Kind))
	}

	users := map[string]bool{}
	for _, u := range c.Users {
		if users[u.Name] {
			errs = append(errs, fmt.Sprintf("duplicate user name %q", u.Name))
		}
		users[u.Name] = true

		hasToken := u.User.Token != ""
		hasPassword := u.User.Username != "" || u.User.Password != ""
		switch {
		case hasToken && hasPassword:
			errs = append(errs, fmt.Sprintf("user %q: both token and username/password are set; choose one", u.Name))
		case !hasToken && !hasPassword:
			errs = append(errs, fmt.Sprintf("user %q: no authentication method configured (set token or username/password)", u.Name))
		}
	}

	nodes := map[string]bool{}
	for _, n := range c.Nodes {
		if nodes[n.Name] {
			errs = append(errs, fmt.Sprintf("duplicate node name %q", n.Name))
		}
		nodes[n.Name] = true

		if n.Node.Server == "" {
			errs = append(errs, fmt.Sprintf("node %q: server is required", n.Name))
			continue
		}
		u, err := url.Parse(n.Node.Server)
		if err != nil || u.Host == "" {
			errs = append(errs, fmt.Sprintf("node %q: invalid server URL %q", n.Name, n.Node.Server))
			continue
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			errs = append(errs, fmt.Sprintf("node %q: server URL scheme must be http or https, got %q", n.Name, u.Scheme))
		}
	}

	contexts := map[string]bool{}
	for _, ctx := range c.Contexts {
		if contexts[ctx.Name] {
			errs = append(errs, fmt.Sprintf("duplicate context name %q", ctx.Name))
		}
		contexts[ctx.Name] = true

		if !users[ctx.Context.User] {
			errs = append(errs, fmt.Sprintf("context %q: user %q not found", ctx.Name, ctx.Context.User))
		}
		if !nodes[ctx.Context.Node] {
			errs = append(errs, fmt.Sprintf("context %q: node %q not found", ctx.Name, ctx.Context.Node))
		}
	}

	if c.CurrentContext != "" && !contexts[c.CurrentContext] {
		errs = append(errs, fmt.Sprintf("current-context: context %q not found", c.CurrentContext))
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid config:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// ResolvedContext is a context with its user and node references resolved.
type ResolvedContext struct {
	Name string
	Node Node
	User User
}

// Resolve returns the named context with its node and user resolved.
// An empty name falls back to current-context.
func (c *Config) Resolve(contextName string) (*ResolvedContext, error) {
	name := contextName
	if name == "" {
		name = c.CurrentContext
	}
	if name == "" {
		return nil, fmt.Errorf("no current context set; use --context or run \"pvectl config use-context <name>\"")
	}

	ctx := c.GetContext(name)
	if ctx == nil {
		return nil, fmt.Errorf("context %q not found (available: %s)", name, strings.Join(c.ContextNames(), ", "))
	}

	user := c.GetUser(ctx.Context.User)
	if user == nil {
		return nil, fmt.Errorf("context %q: user %q not found", name, ctx.Context.User)
	}
	node := c.GetNode(ctx.Context.Node)
	if node == nil {
		return nil, fmt.Errorf("context %q: node %q not found", name, ctx.Context.Node)
	}

	return &ResolvedContext{Name: name, Node: node.Node, User: user.User}, nil
}
