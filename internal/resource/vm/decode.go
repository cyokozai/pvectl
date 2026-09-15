package vm

import (
	"fmt"
	"strings"

	"github.com/cyokozai/pvectl/internal/runtime"
)

// renamedFields maps spec fields that existed in an earlier v1alpha1
// shape to the guidance a migrator needs. Strict decoding reports them
// as "unknown field", which is technically true but unhelpful.
var renamedFields = map[string]string{
	"startOnBoot": "spec.startOnBoot was replaced by spec.runStrategy (Halted | Always | Manual); " +
		"`startOnBoot: true` is now `runStrategy: Always`, and omitting it is `runStrategy: Manual`",
	"password": "spec.cloudInit.password was replaced by spec.cloudInit.passwordFrom, " +
		"which points at the secret instead of carrying it: `passwordFrom: env:PVE_VM_PASSWORD` " +
		"or `passwordFrom: file:/run/secrets/vmpw`",
}

// decodeSpec decodes a manifest's spec, translating renamed fields into
// migration guidance instead of a bare unknown-field error.
func decodeSpec(obj *runtime.Unstructured) (*Spec, error) {
	spec := &Spec{}
	err := obj.DecodeSpec(spec)
	if err == nil {
		return spec, nil
	}
	for field, hint := range renamedFields {
		if strings.Contains(err.Error(), fmt.Sprintf("field %s not found", field)) {
			return nil, fmt.Errorf("%w\n  hint: %s", err, hint)
		}
	}
	return nil, err
}
