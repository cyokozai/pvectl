package config

// Config represents the pvectl configuration file structure.
// Similar to kubeconfig, it manages nodes, users, and contexts.
type Config struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	Users          []NamedUser    `yaml:"users"`
	Nodes          []NamedNode    `yaml:"nodes"`
	Contexts       []NamedContext `yaml:"contexts"`
	CurrentContext string         `yaml:"current-context"`
}

// NamedNode associates a name with a node configuration.
type NamedNode struct {
	Name string `yaml:"name"`
	Node Node   `yaml:"node"`
}

// Node represents a Proxmox VE server connection configuration.
type Node struct {
	Server                string `yaml:"server"`
	InsecureSkipTLSVerify bool   `yaml:"insecureSkipTLSVerify,omitempty"`
}

// NamedUser associates a name with user credentials.
type NamedUser struct {
	Name string `yaml:"name"`
	User User   `yaml:"user"`
}

// User represents authentication credentials for Proxmox VE.
type User struct {
	Token    string `yaml:"token,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

// NamedContext associates a name with a context.
type NamedContext struct {
	Name    string  `yaml:"name"`
	Context Context `yaml:"context"`
}

// Context binds a user to a node.
type Context struct {
	User string `yaml:"user"`
	Node string `yaml:"node"`
}

// NewConfig creates a new empty Config with default values.
func NewConfig() *Config {
	return &Config{
		APIVersion: "v1",
		Kind:       "Config",
		Users:      []NamedUser{},
		Nodes:      []NamedNode{},
		Contexts:   []NamedContext{},
	}
}

// GetContext returns the context with the given name, or nil if not found.
func (c *Config) GetContext(name string) *NamedContext {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			return &c.Contexts[i]
		}
	}
	return nil
}

// GetNode returns the node with the given name, or nil if not found.
func (c *Config) GetNode(name string) *NamedNode {
	for i := range c.Nodes {
		if c.Nodes[i].Name == name {
			return &c.Nodes[i]
		}
	}
	return nil
}

// GetUser returns the user with the given name, or nil if not found.
func (c *Config) GetUser(name string) *NamedUser {
	for i := range c.Users {
		if c.Users[i].Name == name {
			return &c.Users[i]
		}
	}
	return nil
}

// GetCurrentContext returns the current context, or nil if not set.
func (c *Config) GetCurrentContext() *NamedContext {
	if c.CurrentContext == "" {
		return nil
	}
	return c.GetContext(c.CurrentContext)
}

// ContextNames returns all context names.
func (c *Config) ContextNames() []string {
	names := make([]string, len(c.Contexts))
	for i, ctx := range c.Contexts {
		names[i] = ctx.Name
	}
	return names
}
