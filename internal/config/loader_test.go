package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Run("valid config file", func(t *testing.T) {
		path := writeTempConfig(t, `
apiVersion: v1
kind: Config
users:
  - name: admin@pam
    user:
      token: admin@pam!ci=secret
nodes:
  - name: prod
    node:
      server: https://pve.example.com:8006
contexts:
  - name: production
    context:
      user: admin@pam
      node: prod
current-context: production
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.CurrentContext != "production" {
			t.Errorf("CurrentContext = %q, want %q", cfg.CurrentContext, "production")
		}
		if got := cfg.GetUser("admin@pam"); got == nil || got.User.Token == "" {
			t.Errorf("GetUser(admin@pam) = %+v, want token set", got)
		}
		if got := cfg.GetNode("prod"); got == nil || got.Node.Server != "https://pve.example.com:8006" {
			t.Errorf("GetNode(prod) = %+v, want server set", got)
		}
	})

	t.Run("missing file returns empty config", func(t *testing.T) {
		cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent"))
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.APIVersion != "v1" || cfg.Kind != "Config" {
			t.Errorf("empty config = %+v, want defaults", cfg)
		}
	})

	t.Run("malformed yaml returns error", func(t *testing.T) {
		path := writeTempConfig(t, "{{ not yaml")
		if _, err := Load(path); err == nil {
			t.Fatal("Load() error = nil, want parse error")
		}
	})
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
