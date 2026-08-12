package config

import (
	"strings"
	"testing"
)

// validConfig returns a minimal fully-valid config for mutation in tests.
func validConfig() *Config {
	return &Config{
		APIVersion: "v1",
		Kind:       "Config",
		Users: []NamedUser{
			{Name: "admin@pam", User: User{Token: "admin@pam!ci=secret"}},
			{Name: "dev@pam", User: User{Username: "dev@pam", Password: "pass"}},
		},
		Nodes: []NamedNode{
			{Name: "prod", Node: Node{Server: "https://pve.example.com:8006"}},
		},
		Contexts: []NamedContext{
			{Name: "production", Context: Context{User: "admin@pam", Node: "prod"}},
			{Name: "development", Context: Context{User: "dev@pam", Node: "prod"}},
		},
		CurrentContext: "production",
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // substring; "" means valid
	}{
		{"valid config", func(c *Config) {}, ""},
		{"empty current-context is allowed", func(c *Config) { c.CurrentContext = "" }, ""},
		{"wrong apiVersion", func(c *Config) { c.APIVersion = "v2" }, "apiVersion"},
		{"wrong kind", func(c *Config) { c.Kind = "Cluster" }, "kind"},
		{"current-context not found", func(c *Config) { c.CurrentContext = "staging" }, `context "staging" not found`},
		{"context references missing user", func(c *Config) { c.Contexts[0].Context.User = "ghost" }, `user "ghost" not found`},
		{"context references missing node", func(c *Config) { c.Contexts[0].Context.Node = "ghost" }, `node "ghost" not found`},
		{"node server empty", func(c *Config) { c.Nodes[0].Node.Server = "" }, "server is required"},
		{"node server invalid url", func(c *Config) { c.Nodes[0].Node.Server = "://bad" }, "invalid server URL"},
		{"node server unsupported scheme", func(c *Config) { c.Nodes[0].Node.Server = "ftp://pve:8006" }, "http or https"},
		{"user with no credentials", func(c *Config) { c.Users[0].User = User{} }, "no authentication"},
		{"user with token and password", func(c *Config) {
			c.Users[0].User = User{Token: "t", Username: "u", Password: "p"}
		}, "both token and username/password"},
		{"duplicate context names", func(c *Config) { c.Contexts[1].Name = "production" }, "duplicate context"},
		{"duplicate node names", func(c *Config) {
			c.Nodes = append(c.Nodes, NamedNode{Name: "prod", Node: Node{Server: "https://x:8006"}})
		}, "duplicate node"},
		{"duplicate user names", func(c *Config) {
			c.Users = append(c.Users, NamedUser{Name: "admin@pam", User: User{Token: "t"}})
		}, "duplicate user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() error = nil, want substring %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Run("resolves named context", func(t *testing.T) {
		rc, err := validConfig().Resolve("development")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if rc.Name != "development" || rc.User.Username != "dev@pam" || rc.Node.Server != "https://pve.example.com:8006" {
			t.Errorf("Resolve() = %+v, want development context resolved", rc)
		}
	})

	t.Run("empty name falls back to current-context", func(t *testing.T) {
		rc, err := validConfig().Resolve("")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if rc.Name != "production" || rc.User.Token == "" {
			t.Errorf("Resolve() = %+v, want production context resolved", rc)
		}
	})

	t.Run("no current-context and no name", func(t *testing.T) {
		cfg := validConfig()
		cfg.CurrentContext = ""
		if _, err := cfg.Resolve(""); err == nil || !strings.Contains(err.Error(), "no current context") {
			t.Fatalf("Resolve() error = %v, want 'no current context'", err)
		}
	})

	t.Run("unknown context name", func(t *testing.T) {
		if _, err := validConfig().Resolve("staging"); err == nil || !strings.Contains(err.Error(), `"staging" not found`) {
			t.Fatalf("Resolve() error = %v, want not found", err)
		}
	})
}
