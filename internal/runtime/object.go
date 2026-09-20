// Package runtime defines the manifest object envelope shared by all
// resource kinds and the decoder that turns YAML/JSON streams into it.
package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// TypeMeta identifies the schema of a manifest document.
type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
}

// GVK identifies a manifest schema by group, version and kind. It is
// the registry's dispatch key: a kind name alone stops being unique
// once resources are split across API groups (ADR-006).
type GVK struct {
	// Group is the API group, e.g. "pve.io". Empty for group-less
	// apiVersions such as a bare "v1".
	Group string
	// Version is the schema version, e.g. "v1alpha1".
	Version string
	// Kind is the manifest kind, e.g. "VirtualMachine".
	Kind string
}

// APIVersion renders the "group/version" form (or the bare "version"
// when the GVK has no group), as written in a manifest.
func (g GVK) APIVersion() string {
	if g.Group == "" {
		return g.Version
	}
	return g.Group + "/" + g.Version
}

// String renders the GVK for error messages, e.g.
// "pve.io/v1alpha1, Kind=VirtualMachine".
func (g GVK) String() string {
	return fmt.Sprintf("%s, Kind=%s", g.APIVersion(), g.Kind)
}

// GVK parses the type identity of a manifest document. apiVersion is
// accepted as "group/version" (e.g. "pve.io/v1alpha1") or as a bare
// "version" (e.g. "v1"); anything else is an error.
func (t TypeMeta) GVK() (GVK, error) {
	if t.Kind == "" {
		return GVK{}, errors.New(`missing required field "kind"`)
	}
	group, version, err := parseAPIVersion(t.APIVersion)
	if err != nil {
		return GVK{}, err
	}
	return GVK{Group: group, Version: version, Kind: t.Kind}, nil
}

// parseAPIVersion splits an apiVersion into its group and version.
func parseAPIVersion(apiVersion string) (group, version string, err error) {
	if apiVersion == "" {
		return "", "", errors.New(`missing required field "apiVersion"`)
	}
	invalid := fmt.Errorf("invalid apiVersion %q (expected %q or %q)",
		apiVersion, "group/version", "version")
	parts := strings.Split(apiVersion, "/")
	for _, p := range parts {
		if p == "" {
			return "", "", invalid
		}
	}
	switch len(parts) {
	case 1:
		return "", parts[0], nil
	case 2:
		return parts[0], parts[1], nil
	default:
		return "", "", invalid
	}
}

// ObjectMeta holds identifying metadata common to all resources.
type ObjectMeta struct {
	Name        string            `yaml:"name" json:"name"`
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// Unstructured is the dispatch envelope for a decoded manifest document.
// TypeMeta and Metadata are decoded eagerly for registry dispatch; Spec
// stays raw until the resource handler decodes it into its typed spec.
type Unstructured struct {
	TypeMeta `yaml:",inline"`
	Metadata ObjectMeta `yaml:"metadata"`
	Spec     yaml.Node  `yaml:"spec"`
	// Source labels where the document came from (e.g. "vm.yaml[2]",
	// "stdin") for error messages. Not part of the manifest.
	Source string `yaml:"-"`
}

// DecodeSpec decodes the raw spec into the given typed struct.
// Unknown fields are rejected so manifest typos fail fast.
func (u *Unstructured) DecodeSpec(into any) error {
	if u.Spec.Kind == 0 {
		return nil // no spec present; leave the zero value
	}
	raw, err := yaml.Marshal(&u.Spec)
	if err != nil {
		return fmt.Errorf("%s: failed to re-encode spec: %w", u.Source, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s: invalid spec for %s: %w", u.Source, u.Kind, err)
	}
	return nil
}
