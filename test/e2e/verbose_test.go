package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// tokenSecret is the half of the e2e API token that actually
// authenticates. A debug level that printed it would hand the cluster
// to whoever reads the issue the output was pasted into.
const tokenSecret = "00000000-0000-0000-0000-000000000000"

// cloudInitManifest declares a VM whose cloud-init password comes from
// the environment, so the plaintext reaches the API as a cipassword
// parameter — the value -v=9 must mask.
const cloudInitManifest = `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: verbose-vm
spec:
  targetNode: pve1
  vmid: 300
  resources:
    cpu:
      cores: 2
    memory: 1024
  disks:
    - name: scsi0
      size: 8G
      storage: local-lvm
  cloudInit:
    user: admin
    passwordFrom: env:PVECTL_E2E_CIPASSWORD
    sshKeys:
      - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexamplekeymaterial admin@example
`

const cloudInitPassword = "sup3r-s3cret-cloud-init-password"

func writeCloudInitManifest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verbose-vm.yaml")
	if err := os.WriteFile(path, []byte(cloudInitManifest), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PVECTL_E2E_CIPASSWORD", cloudInitPassword)
	return path
}

// TestVerbosityDefaultIsSilent pins the promise that -v costs nothing
// when it is not asked for: the default invocation writes exactly what
// it wrote before the flag existed.
func TestVerbosityDefaultIsSilent(t *testing.T) {
	e := newEnv(t)
	manifest := writeCloudInitManifest(t)

	for _, args := range [][]string{
		{"apply", "-f", manifest},
		{"get", "vm"},
		{"get", "vm", "verbose-vm", "-o", "yaml"},
		{"delete", "vm", "verbose-vm"},
	} {
		_, stderr, err := e.run(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\nstderr: %s", args, err, stderr)
		}
		if stderr != "" {
			t.Errorf("%v wrote to stderr without -v:\n%s", args, stderr)
		}
	}
}

// TestVerbosityLevelsAreCumulative walks the scale and checks each step
// adds what it promises and nothing from the step above it.
func TestVerbosityLevelsAreCumulative(t *testing.T) {
	tests := []struct {
		level    int
		wants    []string
		unwanted []string
	}{
		{
			level: 1,
			wants: []string{"[v1] loading config file ", `[v1] using context "e2e"`, "[v1] authenticating to "},
			// Name resolution is level 2.
			unwanted: []string{"[v2]", "[v6]"},
		},
		{
			level:    2,
			wants:    []string{`[v2] resolved virtualmachine "template-ubuntu" to vmid 100 on node pve1`},
			unwanted: []string{"[v6]"},
		},
		{
			level:    6,
			wants:    []string{"[v6] GET http://", "/api2/json/cluster/resources?type=vm ", "200 OK in "},
			unwanted: []string{"[v7]", "[v8]"},
		},
		{
			level:    7,
			wants:    []string{"[v7] > Authorization: REDACTED", "[v7] < Content-Type: "},
			unwanted: []string{"[v8]"},
		},
		{
			level: 8,
			wants: []string{`[v8] < body: {"data":[`},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("v%d", tt.level), func(t *testing.T) {
			e := newEnv(t)
			_, stderr, err := e.run(t, "get", "vm", "template-ubuntu", fmt.Sprintf("--v=%d", tt.level))
			if err != nil {
				t.Fatalf("get: %v\nstderr: %s", err, stderr)
			}
			for _, want := range tt.wants {
				if !strings.Contains(stderr, want) {
					t.Errorf("-v=%d missing %q:\n%s", tt.level, want, stderr)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(stderr, unwanted) {
					t.Errorf("-v=%d wrote %q, which belongs to a higher level:\n%s", tt.level, unwanted, stderr)
				}
			}
		})
	}
}

// TestVerbosityLogsPlanAndTasks covers the levels that report pvectl's
// own decisions rather than the HTTP traffic.
func TestVerbosityLogsPlanAndTasks(t *testing.T) {
	e := newEnv(t)
	manifest := writeCloudInitManifest(t)

	_, stderr, err := e.run(t, "apply", "-f", manifest, "--v=5")
	if err != nil {
		t.Fatalf("apply: %v\nstderr: %s", err, stderr)
	}

	for _, want := range []string{
		"[v3] virtualmachine/verbose-vm: created",
		"[v4] task start: POST /nodes/pve1/qemu",
		"[v4] task done: POST /nodes/pve1/qemu in ",
		"[v5] POST /nodes/pve1/qemu: params {",
		"cipassword=REDACTED",
		"sshkeys=REDACTED",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("-v=5 missing %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "[v6]") {
		t.Errorf("-v=5 logged HTTP traffic:\n%s", stderr)
	}

	// Changed keys are reported by key, never by value.
	changed := filepath.Join(t.TempDir(), "changed.yaml")
	if err := os.WriteFile(changed, []byte(strings.Replace(cloudInitManifest, "cores: 2", "cores: 4", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err = e.run(t, "apply", "-f", changed, "--v=3"); err != nil {
		t.Fatalf("apply(update): %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "[v3] virtualmachine/verbose-vm: configured, changed keys: cores") {
		t.Errorf("-v=3 did not report the changed key:\n%s", stderr)
	}
}

// TestVerbosityNeverLeaksCredentials is the safety requirement. The
// output of -v is written on the assumption that it will be pasted into
// a public issue, so the strongest level pvectl offers must not carry
// the API token or a cloud-init password — through any route, in either
// stream.
func TestVerbosityNeverLeaksCredentials(t *testing.T) {
	e := newEnv(t)
	manifest := writeCloudInitManifest(t)

	secrets := map[string]string{
		"the whole API token":     token,
		"the token secret":        tokenSecret,
		"the cloud-init password": cloudInitPassword,
	}

	for _, args := range [][]string{
		{"apply", "-f", manifest, "--v=9"},
		{"get", "vm", "-o", "yaml", "--v=9"},
		{"describe", "vm", "verbose-vm", "--v=9"},
		{"delete", "vm", "verbose-vm", "--v=9"},
	} {
		stdout, stderr, err := e.run(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\nstderr: %s", args, err, stderr)
		}
		for what, secret := range secrets {
			if strings.Contains(stderr, secret) {
				t.Fatalf("%v leaked %s to stderr:\n%s", args, what, stderr)
			}
			if strings.Contains(stdout, secret) {
				t.Fatalf("%v leaked %s to stdout:\n%s", args, what, stdout)
			}
		}
		if !strings.Contains(stderr, "REDACTED") {
			t.Errorf("%v redacted nothing, so this proves nothing:\n%s", args, stderr)
		}
	}
}

// TestVerbosityLeavesStdoutParseable pins the reason debug output goes
// to stderr: -o yaml and -o json must stay machine-readable however
// loud -v is.
func TestVerbosityLeavesStdoutParseable(t *testing.T) {
	e := newEnv(t)

	stdoutQuiet, _, err := e.run(t, "get", "vm", "template-ubuntu", "-o", "yaml")
	if err != nil {
		t.Fatalf("get -o yaml: %v", err)
	}

	stdoutLoud, stderr, err := e.run(t, "get", "vm", "template-ubuntu", "-o", "yaml", "--v=9")
	if err != nil {
		t.Fatalf("get -o yaml --v=9: %v\nstderr: %s", err, stderr)
	}

	if stdoutLoud != stdoutQuiet {
		t.Errorf("-v=9 changed stdout:\nwithout -v:\n%s\nwith -v=9:\n%s", stdoutQuiet, stdoutLoud)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(stdoutLoud), &doc); err != nil {
		t.Fatalf("stdout is not parseable YAML with -v=9: %v\n%s", err, stdoutLoud)
	}
	if doc["kind"] != "VirtualMachine" {
		t.Errorf("parsed YAML = %v", doc)
	}
	if stderr == "" {
		t.Error("-v=9 wrote nothing to stderr")
	}
}

// TestVerbosityRejectsNothingBelowZero: a negative level is treated as
// silence rather than an error, matching how kubectl ignores it.
func TestVerbosityNegativeIsSilent(t *testing.T) {
	e := newEnv(t)
	_, stderr, err := e.run(t, "get", "vm", "--v=-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stderr != "" {
		t.Errorf("--v=-1 wrote to stderr:\n%s", stderr)
	}
}
