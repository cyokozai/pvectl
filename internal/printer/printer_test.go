package printer

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

// stubVM is a minimal printable object for tests.
type stubVM struct {
	Kind   string `yaml:"kind" json:"kind"`
	Name   string `yaml:"name" json:"name"`
	Status string `yaml:"status" json:"status"`
	Node   string `yaml:"node" json:"node"`
}

func (s stubVM) GetKind() string { return s.Kind }
func (s stubVM) GetName() string { return s.Name }

func stubColumns() []Column {
	return []Column{
		{Name: "NAME", Extract: func(o Object) string { return o.(stubVM).Name }},
		{Name: "STATUS", Extract: func(o Object) string { return o.(stubVM).Status }},
		{Name: "NODE", Extract: func(o Object) string { return o.(stubVM).Node }, WideOnly: true},
	}
}

func stubObjects() []Object {
	return []Object{
		stubVM{Kind: "VirtualMachine", Name: "web-server", Status: "running", Node: "pve1"},
		stubVM{Kind: "VirtualMachine", Name: "db", Status: "stopped", Node: "pve2"},
	}
}

func TestParseFormat(t *testing.T) {
	for in, want := range map[string]Format{
		"table": FormatTable, "wide": FormatWide, "yaml": FormatYAML,
		"json": FormatJSON, "name": FormatName, "": FormatTable,
	} {
		got, err := ParseFormat(in)
		if err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %v, %v; want %v, nil", in, got, err, want)
		}
	}
	if _, err := ParseFormat("xml"); err == nil || !strings.Contains(err.Error(), "xml") {
		t.Errorf("ParseFormat(xml) error = %v, want unsupported-format error", err)
	}
}

func TestPrintGolden(t *testing.T) {
	for _, format := range []Format{FormatTable, FormatWide, FormatYAML, FormatJSON, FormatName} {
		t.Run(string(format), func(t *testing.T) {
			var sb strings.Builder
			if err := Print(&sb, format, stubObjects(), stubColumns()); err != nil {
				t.Fatalf("Print() error = %v", err)
			}
			compareGolden(t, filepath.Join("testdata", string(format)+".golden"), sb.String())
		})
	}
}

func TestPrintEmptyTable(t *testing.T) {
	var sb strings.Builder
	if err := Print(&sb, FormatTable, nil, stubColumns()); err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	if !strings.Contains(sb.String(), "No resources found") {
		t.Errorf("Print(empty) = %q, want 'No resources found'", sb.String())
	}
}

func TestPrintSingleYAMLHasNoSeparator(t *testing.T) {
	var sb strings.Builder
	if err := Print(&sb, FormatYAML, stubObjects()[:1], nil); err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	if strings.Contains(sb.String(), "---") {
		t.Errorf("single-object YAML must not contain a document separator:\n%s", sb.String())
	}
}

func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file %s (run go test ./internal/printer -update): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("output mismatch for %s:\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
