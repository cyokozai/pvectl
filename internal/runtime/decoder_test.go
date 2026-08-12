package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vmManifest = `
apiVersion: pve.io/v1alpha1
kind: VirtualMachine
metadata:
  name: web-server
  labels:
    app: web
spec:
  targetNode: pve1
  resources:
    memory: 2048
`

func TestDecode(t *testing.T) {
	t.Run("single document", func(t *testing.T) {
		objs, err := Decode(strings.NewReader(vmManifest), "vm.yaml")
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if len(objs) != 1 {
			t.Fatalf("Decode() returned %d objects, want 1", len(objs))
		}
		obj := objs[0]
		if obj.APIVersion != "pve.io/v1alpha1" || obj.Kind != "VirtualMachine" {
			t.Errorf("TypeMeta = %+v, want pve.io/v1alpha1 VirtualMachine", obj.TypeMeta)
		}
		if obj.Metadata.Name != "web-server" || obj.Metadata.Labels["app"] != "web" {
			t.Errorf("Metadata = %+v, want name/labels", obj.Metadata)
		}
		if obj.Source != "vm.yaml" {
			t.Errorf("Source = %q, want vm.yaml", obj.Source)
		}
	})

	t.Run("deferred spec decode", func(t *testing.T) {
		objs, err := Decode(strings.NewReader(vmManifest), "vm.yaml")
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		var spec struct {
			TargetNode string `yaml:"targetNode"`
			Resources  struct {
				Memory int `yaml:"memory"`
			} `yaml:"resources"`
		}
		if err := objs[0].DecodeSpec(&spec); err != nil {
			t.Fatalf("DecodeSpec() error = %v", err)
		}
		if spec.TargetNode != "pve1" || spec.Resources.Memory != 2048 {
			t.Errorf("spec = %+v, want targetNode=pve1 memory=2048", spec)
		}
	})

	t.Run("multi-document stream", func(t *testing.T) {
		multi := vmManifest + "\n---\n" + strings.Replace(vmManifest, "web-server", "db-server", 1)
		objs, err := Decode(strings.NewReader(multi), "multi.yaml")
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if len(objs) != 2 {
			t.Fatalf("Decode() returned %d objects, want 2", len(objs))
		}
		if objs[0].Metadata.Name != "web-server" || objs[1].Metadata.Name != "db-server" {
			t.Errorf("names = %q, %q", objs[0].Metadata.Name, objs[1].Metadata.Name)
		}
		if objs[0].Source != "multi.yaml[0]" || objs[1].Source != "multi.yaml[1]" {
			t.Errorf("sources = %q, %q, want indexed", objs[0].Source, objs[1].Source)
		}
	})

	t.Run("empty documents are skipped", func(t *testing.T) {
		objs, err := Decode(strings.NewReader("---\n"+vmManifest+"\n---\n# comment only\n"), "vm.yaml")
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if len(objs) != 1 {
			t.Fatalf("Decode() returned %d objects, want 1", len(objs))
		}
	})

	t.Run("JSON document", func(t *testing.T) {
		jsonDoc := `{
  "apiVersion": "pve.io/v1alpha1",
  "kind": "VirtualMachine",
  "metadata": {"name": "json-vm"},
  "spec": {"targetNode": "pve1"}
}`
		objs, err := Decode(strings.NewReader(jsonDoc), "vm.json")
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if len(objs) != 1 || objs[0].Metadata.Name != "json-vm" {
			t.Fatalf("Decode(json) = %+v, want json-vm", objs)
		}
	})

	t.Run("missing kind is an error naming the source", func(t *testing.T) {
		_, err := Decode(strings.NewReader("metadata:\n  name: x\n"), "bad.yaml")
		if err == nil || !strings.Contains(err.Error(), "bad.yaml") || !strings.Contains(err.Error(), "kind") {
			t.Fatalf("Decode() error = %v, want kind error naming bad.yaml", err)
		}
	})

	t.Run("malformed yaml is an error", func(t *testing.T) {
		if _, err := Decode(strings.NewReader("{{ nope"), "bad.yaml"); err == nil {
			t.Fatal("Decode() error = nil, want parse error")
		}
	})
}

func TestDecodeFiles(t *testing.T) {
	t.Run("multiple files preserve order", func(t *testing.T) {
		dir := t.TempDir()
		a := writeFile(t, dir, "a.yaml", vmManifest)
		b := writeFile(t, dir, "b.yaml", strings.Replace(vmManifest, "web-server", "b-vm", 1))
		objs, err := DecodeFiles([]string{b, a})
		if err != nil {
			t.Fatalf("DecodeFiles() error = %v", err)
		}
		if len(objs) != 2 || objs[0].Metadata.Name != "b-vm" || objs[1].Metadata.Name != "web-server" {
			t.Fatalf("DecodeFiles() order wrong: %+v", names(objs))
		}
	})

	t.Run("directory reads manifest files sorted", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "b.yml", strings.Replace(vmManifest, "web-server", "b-vm", 1))
		writeFile(t, dir, "a.yaml", vmManifest)
		writeFile(t, dir, "ignore.txt", "not a manifest")
		writeFile(t, dir, "c.json", `{"apiVersion":"pve.io/v1alpha1","kind":"VirtualMachine","metadata":{"name":"c-vm"},"spec":{}}`)
		objs, err := DecodeFiles([]string{dir})
		if err != nil {
			t.Fatalf("DecodeFiles() error = %v", err)
		}
		if got := names(objs); len(got) != 3 || got[0] != "web-server" || got[1] != "b-vm" || got[2] != "c-vm" {
			t.Fatalf("DecodeFiles(dir) = %v, want [web-server b-vm c-vm]", got)
		}
	})

	t.Run("stdin via dash", func(t *testing.T) {
		old := stdin
		stdin = strings.NewReader(vmManifest)
		defer func() { stdin = old }()
		objs, err := DecodeFiles([]string{"-"})
		if err != nil {
			t.Fatalf("DecodeFiles(-) error = %v", err)
		}
		if len(objs) != 1 || objs[0].Source != "stdin" {
			t.Fatalf("DecodeFiles(-) = %+v, want 1 object from stdin", names(objs))
		}
	})

	t.Run("missing file is an error", func(t *testing.T) {
		if _, err := DecodeFiles([]string{"/nonexistent/vm.yaml"}); err == nil {
			t.Fatal("DecodeFiles() error = nil, want error")
		}
	})
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func names(objs []*Unstructured) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.Metadata.Name
	}
	return out
}
