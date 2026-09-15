package cliopt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyokozai/pvectl/internal/api"
	"github.com/cyokozai/pvectl/internal/config"
)

const testConfigYAML = `
apiVersion: v1
kind: Config
users:
  - name: admin@pam
    user:
      token: admin@pam!ci=secret
nodes:
  - name: prod
    node:
      server: https://prod.example.com:8006
  - name: dev
    node:
      server: https://dev.example.com:8006
contexts:
  - name: production
    context:
      user: admin@pam
      node: prod
  - name: development
    context:
      user: admin@pam
      node: dev
current-context: production
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFactoryConfig(t *testing.T) {
	t.Run("explicit --config path wins", func(t *testing.T) {
		f := &Factory{ConfigPath: writeConfig(t, testConfigYAML)}
		cfg, err := f.Config()
		if err != nil {
			t.Fatalf("Config() error = %v", err)
		}
		if cfg.CurrentContext != "production" {
			t.Errorf("CurrentContext = %q", cfg.CurrentContext)
		}
	})

	t.Run("PVECTL_CONFIG env is used when flag is empty", func(t *testing.T) {
		t.Setenv("PVECTL_CONFIG", writeConfig(t, testConfigYAML))
		f := &Factory{}
		cfg, err := f.Config()
		if err != nil {
			t.Fatalf("Config() error = %v", err)
		}
		if len(cfg.Contexts) != 2 {
			t.Errorf("Contexts = %d, want 2", len(cfg.Contexts))
		}
	})

	t.Run("invalid config fails validation", func(t *testing.T) {
		f := &Factory{ConfigPath: writeConfig(t, strings.Replace(testConfigYAML, "node: prod", "node: ghost", 1))}
		if _, err := f.Config(); err == nil || !strings.Contains(err.Error(), "ghost") {
			t.Fatalf("Config() error = %v, want validation error", err)
		}
	})
}

func TestFactoryCurrentContext(t *testing.T) {
	t.Run("defaults to current-context", func(t *testing.T) {
		f := &Factory{ConfigPath: writeConfig(t, testConfigYAML)}
		rc, err := f.CurrentContext()
		if err != nil {
			t.Fatalf("CurrentContext() error = %v", err)
		}
		if rc.Name != "production" || rc.Node.Server != "https://prod.example.com:8006" {
			t.Errorf("resolved = %+v", rc)
		}
	})

	t.Run("--context flag overrides", func(t *testing.T) {
		f := &Factory{ConfigPath: writeConfig(t, testConfigYAML), ContextName: "development"}
		rc, err := f.CurrentContext()
		if err != nil {
			t.Fatalf("CurrentContext() error = %v", err)
		}
		if rc.Name != "development" || rc.Node.Server != "https://dev.example.com:8006" {
			t.Errorf("resolved = %+v", rc)
		}
	})

	t.Run("PVECTL_CONTEXT env overrides current-context but not the flag", func(t *testing.T) {
		t.Setenv("PVECTL_CONTEXT", "development")
		f := &Factory{ConfigPath: writeConfig(t, testConfigYAML)}
		rc, err := f.CurrentContext()
		if err != nil || rc.Name != "development" {
			t.Fatalf("CurrentContext() = %+v, %v; want development from env", rc, err)
		}

		f2 := &Factory{ConfigPath: writeConfig(t, testConfigYAML), ContextName: "production"}
		rc2, err := f2.CurrentContext()
		if err != nil || rc2.Name != "production" {
			t.Fatalf("CurrentContext() = %+v, %v; want flag to beat env", rc2, err)
		}
	})
}

func TestFactoryClient(t *testing.T) {
	calls := 0
	f := &Factory{
		ConfigPath: writeConfig(t, testConfigYAML),
		NewClient: func(ctx context.Context, node config.Node, user config.User, opts api.Options) (api.Client, error) {
			calls++
			if node.Server != "https://prod.example.com:8006" || user.Token == "" {
				t.Errorf("NewClient got node=%+v user token empty=%v", node, user.Token == "")
			}
			return nil, nil
		},
	}

	if _, err := f.Client(context.Background()); err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	if _, err := f.Client(context.Background()); err != nil {
		t.Fatalf("Client() second call error = %v", err)
	}
	if calls != 1 {
		t.Errorf("NewClient called %d times, want memoized single call", calls)
	}
}
