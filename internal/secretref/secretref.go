// Package secretref resolves external references to secret values so
// manifests can stay in version control without carrying plaintext.
//
// Two forms are understood:
//
//	env:NAME            the NAME environment variable
//	file:/path/to/file  the file's contents, trailing newline trimmed
//
// The syntax can be checked without touching the environment
// (Validate), which keeps client-side dry-runs independent of where
// they run.
package secretref

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Reference schemes.
const (
	EnvPrefix  = "env:"
	FilePrefix = "file:"
)

// ErrUnresolved means the reference is well-formed but its target does
// not exist or is empty.
var ErrUnresolved = errors.New("secret reference could not be resolved")

// Validate checks the syntax of a reference without reading anything.
func Validate(ref string) error {
	switch {
	case strings.HasPrefix(ref, EnvPrefix):
		if strings.TrimSpace(strings.TrimPrefix(ref, EnvPrefix)) == "" {
			return fmt.Errorf("%q names no environment variable (expected env:VARIABLE)", ref)
		}
		return nil
	case strings.HasPrefix(ref, FilePrefix):
		if strings.TrimSpace(strings.TrimPrefix(ref, FilePrefix)) == "" {
			return fmt.Errorf("%q names no file (expected file:/path/to/file)", ref)
		}
		return nil
	default:
		return fmt.Errorf("%q is not a secret reference (expected env:VARIABLE or file:/path/to/file)", ref)
	}
}

// Resolve returns the referenced secret. Syntax problems and missing
// targets are both errors; missing targets wrap ErrUnresolved so
// callers can treat "the manifest is fine, this machine is not"
// differently from "the manifest is wrong".
func Resolve(ref string) (string, error) {
	if err := Validate(ref); err != nil {
		return "", err
	}
	switch {
	case strings.HasPrefix(ref, EnvPrefix):
		name := strings.TrimPrefix(ref, EnvPrefix)
		value, ok := os.LookupEnv(name)
		if !ok || value == "" {
			return "", fmt.Errorf("%w: environment variable %s is not set", ErrUnresolved, name)
		}
		return value, nil
	default:
		path := strings.TrimPrefix(ref, FilePrefix)
		data, err := os.ReadFile(path) //nolint:gosec // the path is the user's own manifest
		if err != nil {
			return "", fmt.Errorf("%w: %s", ErrUnresolved, err)
		}
		// Files written by `echo` or an editor end in a newline that is
		// not part of the secret.
		value := strings.TrimRight(string(data), "\r\n")
		if value == "" {
			return "", fmt.Errorf("%w: file %s is empty", ErrUnresolved, path)
		}
		return value, nil
	}
}
