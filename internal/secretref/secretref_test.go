package secretref

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	valid := []string{"env:PVE_VM_PASSWORD", "file:/run/secrets/vmpw", "file:relative/path"}
	for _, ref := range valid {
		if err := Validate(ref); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", ref, err)
		}
	}

	invalid := map[string]string{
		"":                  "not a secret reference",
		"plaintext":         "not a secret reference",
		"vault:secret/path": "not a secret reference",
		"env:":              "no environment variable",
		"file:":             "no file",
	}
	for ref, want := range invalid {
		err := Validate(ref)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate(%q) = %v, want an error mentioning %q", ref, err, want)
		}
	}
}

func TestResolveEnv(t *testing.T) {
	t.Setenv("PVECTL_TEST_SECRET", "s3cret")

	got, err := Resolve("env:PVECTL_TEST_SECRET")
	if err != nil || got != "s3cret" {
		t.Fatalf("Resolve() = %q, %v", got, err)
	}

	_, err = Resolve("env:PVECTL_TEST_MISSING")
	if !errors.Is(err, ErrUnresolved) {
		t.Errorf("Resolve(missing) error = %v, want ErrUnresolved", err)
	}

	t.Setenv("PVECTL_TEST_EMPTY", "")
	if _, err := Resolve("env:PVECTL_TEST_EMPTY"); !errors.Is(err, ErrUnresolved) {
		t.Errorf("Resolve(empty) error = %v, want ErrUnresolved", err)
	}
}

func TestResolveFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmpw")
	// A trailing newline is an artifact of how the file was written,
	// not part of the secret.
	if err := os.WriteFile(path, []byte("s3cret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve("file:" + path)
	if err != nil || got != "s3cret" {
		t.Fatalf("Resolve() = %q, %v", got, err)
	}

	crlf := filepath.Join(dir, "crlf")
	if err := os.WriteFile(crlf, []byte("s3cret\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := Resolve("file:" + crlf); err != nil || got != "s3cret" {
		t.Errorf("Resolve(crlf) = %q, %v", got, err)
	}

	// Interior whitespace belongs to the secret.
	spaced := filepath.Join(dir, "spaced")
	if err := os.WriteFile(spaced, []byte("  pass word  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := Resolve("file:" + spaced); err != nil || got != "  pass word  " {
		t.Errorf("Resolve(spaced) = %q, %v", got, err)
	}

	if _, err := Resolve("file:" + filepath.Join(dir, "ghost")); !errors.Is(err, ErrUnresolved) {
		t.Errorf("Resolve(missing file) error = %v, want ErrUnresolved", err)
	}

	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("file:" + empty); !errors.Is(err, ErrUnresolved) {
		t.Errorf("Resolve(empty file) error = %v, want ErrUnresolved", err)
	}
}

func TestResolveRejectsBadSyntaxBeforeReading(t *testing.T) {
	_, err := Resolve("plaintext-password")
	if err == nil || errors.Is(err, ErrUnresolved) {
		t.Fatalf("Resolve() error = %v, want a syntax error (not ErrUnresolved)", err)
	}
}
