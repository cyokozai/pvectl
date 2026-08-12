// Package printer renders resource objects in kubectl-style output
// formats: table, wide, yaml, json, and name.
package printer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format is an output format selected with -o.
type Format string

// Supported output formats.
const (
	FormatTable Format = "table"
	FormatWide  Format = "wide"
	FormatYAML  Format = "yaml"
	FormatJSON  Format = "json"
	FormatName  Format = "name"
)

// ParseFormat parses the -o flag value. Empty means table.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case "":
		return FormatTable, nil
	case FormatTable, FormatWide, FormatYAML, FormatJSON, FormatName:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unsupported output format %q (supported: table, wide, yaml, json, name)", s)
	}
}

// Object is the minimal contract an object must satisfy to be printed.
// yaml/json formats marshal the object itself, so handlers should return
// manifest-shaped structs that round-trip into apply -f.
type Object interface {
	GetKind() string
	GetName() string
}

// Column describes one table column for a resource kind.
type Column struct {
	Name     string
	Extract  func(obj Object) string
	WideOnly bool
}

// Print renders objs to w in the given format.
func Print(w io.Writer, format Format, objs []Object, cols []Column) error {
	switch format {
	case FormatTable, FormatWide:
		return printTable(w, objs, cols, format == FormatWide)
	case FormatYAML:
		return printYAML(w, objs)
	case FormatJSON:
		return printJSON(w, objs)
	case FormatName:
		return printName(w, objs)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func printTable(w io.Writer, objs []Object, cols []Column, wide bool) error {
	if len(objs) == 0 {
		_, err := fmt.Fprintln(w, "No resources found.")
		return err
	}

	visible := make([]Column, 0, len(cols))
	for _, c := range cols {
		if c.WideOnly && !wide {
			continue
		}
		visible = append(visible, c)
	}

	tw := tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
	headers := make([]string, len(visible))
	for i, c := range visible {
		headers[i] = c.Name
	}
	if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
		return err
	}
	for _, obj := range objs {
		cells := make([]string, len(visible))
		for i, c := range visible {
			cells[i] = c.Extract(obj)
		}
		if _, err := fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func printYAML(w io.Writer, objs []Object) error {
	for i, obj := range objs {
		if i > 0 {
			if _, err := fmt.Fprintln(w, "---"); err != nil {
				return err
			}
		}
		data, err := yaml.Marshal(obj)
		if err != nil {
			return fmt.Errorf("failed to encode %s/%s: %w", strings.ToLower(obj.GetKind()), obj.GetName(), err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

func printJSON(w io.Writer, objs []Object) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if len(objs) == 1 {
		return enc.Encode(objs[0])
	}
	return enc.Encode(objs)
}

func printName(w io.Writer, objs []Object) error {
	for _, obj := range objs {
		if _, err := fmt.Fprintf(w, "%s/%s\n", strings.ToLower(obj.GetKind()), obj.GetName()); err != nil {
			return err
		}
	}
	return nil
}
