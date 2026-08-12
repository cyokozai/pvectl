package runtime

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// stdin is swappable for tests of the "-f -" path.
var stdin io.Reader = os.Stdin

// manifestExtensions are the file extensions read from a directory argument.
var manifestExtensions = map[string]bool{".yaml": true, ".yml": true, ".json": true}

// Decode reads a (possibly multi-document) YAML or JSON stream and returns
// one Unstructured per non-empty document. source labels error messages
// and is suffixed with "[i]" when the stream holds multiple documents.
func Decode(r io.Reader, source string) ([]*Unstructured, error) {
	dec := yaml.NewDecoder(r)

	var objs []*Unstructured
	for i := 0; ; i++ {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: failed to parse document %d: %w", source, i, err)
		}
		if isEmptyDocument(&node) {
			continue
		}

		obj := &Unstructured{}
		if err := node.Decode(obj); err != nil {
			return nil, fmt.Errorf("%s: failed to decode document %d: %w", source, i, err)
		}
		objs = append(objs, obj)
	}

	for i, obj := range objs {
		obj.Source = source
		if len(objs) > 1 {
			obj.Source = fmt.Sprintf("%s[%d]", source, i)
		}
		if obj.Kind == "" {
			return nil, fmt.Errorf("%s: missing required field \"kind\"", obj.Source)
		}
		if obj.APIVersion == "" {
			return nil, fmt.Errorf("%s: missing required field \"apiVersion\"", obj.Source)
		}
	}
	return objs, nil
}

// DecodeFiles decodes manifests from the given paths in order.
// "-" reads stdin; a directory reads its *.yaml/*.yml/*.json entries sorted
// by name (non-recursive, kubectl behavior).
func DecodeFiles(paths []string) ([]*Unstructured, error) {
	var objs []*Unstructured
	for _, path := range paths {
		switch path {
		case "-":
			decoded, err := Decode(stdin, "stdin")
			if err != nil {
				return nil, err
			}
			objs = append(objs, decoded...)
		default:
			info, err := os.Stat(path)
			if err != nil {
				return nil, fmt.Errorf("cannot read %q: %w", path, err)
			}
			if info.IsDir() {
				decoded, err := decodeDir(path)
				if err != nil {
					return nil, err
				}
				objs = append(objs, decoded...)
				continue
			}
			decoded, err := decodeFile(path)
			if err != nil {
				return nil, err
			}
			objs = append(objs, decoded...)
		}
	}
	return objs, nil
}

func decodeDir(dir string) ([]*Unstructured, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %q: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !manifestExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)

	var objs []*Unstructured
	for _, f := range files {
		decoded, err := decodeFile(f)
		if err != nil {
			return nil, err
		}
		objs = append(objs, decoded...)
	}
	return objs, nil
}

func decodeFile(path string) ([]*Unstructured, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %q: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only file
	return Decode(f, path)
}

// isEmptyDocument reports whether a decoded document node holds no content
// (empty document, comment-only document, or explicit null).
func isEmptyDocument(node *yaml.Node) bool {
	if node.Kind == 0 {
		return true
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return true
		}
		c := node.Content[0]
		return c.Kind == yaml.ScalarNode && c.Tag == "!!null"
	}
	return false
}
