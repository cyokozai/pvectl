package diff

import (
	"strings"
	"testing"
)

func TestCompute(t *testing.T) {
	t.Run("only desired keys are compared", func(t *testing.T) {
		current := map[string]any{"cores": 2, "memory": 2048, "vmgenid": "abc", "digest": "xyz"}
		desired := map[string]any{"cores": 4}
		res := Compute(current, desired, nil)
		if len(res.Entries) != 1 {
			t.Fatalf("Entries = %+v, want exactly 1", res.Entries)
		}
		e := res.Entries[0]
		if e.Key != "cores" || e.Current != "2" || e.Desired != "4" {
			t.Errorf("Entry = %+v, want cores 2->4", e)
		}
	})

	t.Run("equal values produce no entry regardless of Go type", func(t *testing.T) {
		current := map[string]any{"memory": float64(2048), "name": "web", "onboot": float64(1)}
		desired := map[string]any{"memory": 2048, "name": "web", "onboot": true}
		res := Compute(current, desired, nil)
		if !res.Empty() {
			t.Fatalf("Entries = %+v, want empty", res.Entries)
		}
	})

	t.Run("key missing in current", func(t *testing.T) {
		res := Compute(map[string]any{}, map[string]any{"ciuser": "admin"}, nil)
		if len(res.Entries) != 1 || res.Entries[0].Current != "" || res.Entries[0].Desired != "admin" {
			t.Fatalf("Entries = %+v, want ciuser added", res.Entries)
		}
	})

	t.Run("rules normalize before comparing", func(t *testing.T) {
		rules := Rules{
			"tags": func(cur, des string) (string, string, bool) {
				return sortJoin(cur), sortJoin(des), false
			},
		}
		res := Compute(
			map[string]any{"tags": "web;prod"},
			map[string]any{"tags": "prod;web"},
			rules,
		)
		if !res.Empty() {
			t.Fatalf("Entries = %+v, want empty after normalization", res.Entries)
		}
	})

	t.Run("rules can skip write-only keys", func(t *testing.T) {
		rules := Rules{
			"cipassword": func(cur, des string) (string, string, bool) { return "", "", true },
		}
		res := Compute(
			map[string]any{"cipassword": "**********"},
			map[string]any{"cipassword": "secret"},
			rules,
		)
		if !res.Empty() {
			t.Fatalf("Entries = %+v, want cipassword skipped", res.Entries)
		}
	})

	t.Run("entries are sorted by key", func(t *testing.T) {
		res := Compute(
			map[string]any{"b": 1, "a": 1, "c": 1},
			map[string]any{"b": 2, "a": 2, "c": 2},
			nil,
		)
		if len(res.Entries) != 3 || res.Entries[0].Key != "a" || res.Entries[2].Key != "c" {
			t.Fatalf("Entries = %+v, want sorted a,b,c", res.Entries)
		}
	})
}

func TestRender(t *testing.T) {
	res := Compute(
		map[string]any{"cores": 2},
		map[string]any{"cores": 4, "ciuser": "admin"},
		nil,
	)
	var sb strings.Builder
	if err := res.Render(&sb); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	out := sb.String()
	for _, want := range []string{"--- live", "+++ desired", "-cores: 2", "+cores: 4", "+ciuser: admin"} {
		if !strings.Contains(out, want) {
			t.Errorf("Render() output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "-ciuser") {
		t.Errorf("Render() must not emit a removal line for a key absent in live:\n%s", out)
	}
}

func sortJoin(s string) string {
	parts := strings.Split(s, ";")
	for i := range parts {
		for j := i + 1; j < len(parts); j++ {
			if parts[j] < parts[i] {
				parts[i], parts[j] = parts[j], parts[i]
			}
		}
	}
	return strings.Join(parts, ";")
}
