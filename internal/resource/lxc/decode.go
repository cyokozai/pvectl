package lxc

import (
	"fmt"
	"strings"

	"github.com/cyokozai/pvectl/internal/runtime"
)

// fieldHints maps spec fields people reach for by habit to where the
// value actually goes. Strict decoding reports them as "unknown field",
// which is true but unhelpful.
var fieldHints = map[string]string{
	"hostname": "a LinuxContainer's hostname is metadata.name; there is no separate spec.hostname (ADR-010 §1)",
	"password": "the root password goes in spec.root.passwordFrom, which points at the secret instead of carrying it: " +
		"`passwordFrom: env:CT_ROOT_PASSWORD` or `passwordFrom: file:/run/secrets/ctpw`",
}

// decodeSpec strictly decodes a manifest's spec (unknown fields are
// errors), adding a hint for the common misplaced fields.
func decodeSpec(obj *runtime.Unstructured) (*Spec, error) {
	spec := &Spec{}
	err := obj.DecodeSpec(spec)
	if err == nil {
		return spec, nil
	}
	for field, hint := range fieldHints {
		if strings.Contains(err.Error(), fmt.Sprintf("field %s not found", field)) {
			return nil, fmt.Errorf("%w\n  hint: %s", err, hint)
		}
	}
	return nil, err
}
