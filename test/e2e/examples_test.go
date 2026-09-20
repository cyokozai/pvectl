package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cyokozai/pvectl/internal/config"
)

// TestExampleManifestsAreValid keeps examples/ from rotting: every
// manifest must decode and validate (client dry-run needs no server).
func TestExampleManifestsAreValid(t *testing.T) {
	entries, err := os.ReadDir("../../examples")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	for _, entry := range entries {
		name := entry.Name()
		if name == "config.yaml" || filepath.Ext(name) != ".yaml" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			_, stderr, err := e.run(t, "apply", "-f", filepath.Join("../../examples", name), "--dry-run=client")
			if err != nil {
				t.Errorf("apply --dry-run=client failed: %v\nstderr: %s", err, stderr)
			}
		})
	}
}

// TestExampleConfigIsValid validates examples/config.yaml against the
// loader and validator.
func TestExampleConfigIsValid(t *testing.T) {
	cfg, err := config.Load("../../examples/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
