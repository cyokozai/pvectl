// Package runtime defines the manifest object envelope shared by all
// resource kinds and the decoder that turns YAML/JSON streams into it.
package runtime

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// TypeMeta identifies the schema of a manifest document.
type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
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
