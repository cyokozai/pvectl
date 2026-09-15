package diff

import (
	"fmt"
	"io"
)

// Render writes the result as a unified-style diff of "key: value" lines.
// Keys absent from the live config only emit an addition line.
func (r Result) Render(w io.Writer) error {
	if r.Empty() {
		return nil
	}
	if _, err := fmt.Fprintln(w, "--- live"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "+++ desired"); err != nil {
		return err
	}
	for _, e := range r.Entries {
		if e.Current != "" {
			if _, err := fmt.Fprintf(w, "-%s: %s\n", e.Key, e.Current); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "+%s: %s\n", e.Key, e.Desired); err != nil {
			return err
		}
	}
	return nil
}
