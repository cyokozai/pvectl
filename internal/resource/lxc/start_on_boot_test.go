package lxc

import (
	"context"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/cyokozai/pvectl/internal/api/apitest"
	"github.com/cyokozai/pvectl/internal/resource"
)

// withPower replaces existingCT's power lines (runStrategy: Always,
// startOnBoot: true) with the given spec lines; an empty string drops
// them.
func withPower(lines string) string {
	return strings.Replace(existing(2, "8G"), "  runStrategy: Always\n  startOnBoot: true\n", lines, 1)
}

// TestExportStoppedOnbootContainerRoundTrips is the ADR-005 §5 bug this
// field exists for: a container with onboot=1 that is stopped for
// maintenance must export to a manifest that applies unchanged and never
// starts it.
func TestExportStoppedOnbootContainerRoundTrips(t *testing.T) {
	for label, onboot := range map[string]any{"onboot=1": float64(1), "onboot=0": float64(0), "onboot absent": nil} {
		t.Run(label, func(t *testing.T) {
			f := seededFake()
			f.GuestList[0].Status = "stopped"
			if onboot == nil {
				delete(f.Configs[200], "onboot")
			} else {
				f.Configs[200]["onboot"] = onboot
			}

			obj, err := NewHandler().Get(context.Background(), f, "web-01")
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			data, err := yaml.Marshal(obj)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "runStrategy") {
				t.Errorf("export must not carry runStrategy:\n%s", data)
			}
			want := "startOnBoot: false"
			if onboot == float64(1) {
				want = "startOnBoot: true"
			}
			if !strings.Contains(string(data), want) {
				t.Errorf("export lacks %q:\n%s", want, data)
			}

			res, err := apply(t, f, string(data), resource.DryRunNone)
			if err != nil {
				t.Fatalf("Apply(export) error = %v\nyaml:\n%s", err, data)
			}
			if res.Action != resource.ActionUnchanged {
				t.Errorf("Action = %v, want unchanged (diff %+v)\nyaml:\n%s", res.Action, res.Diff, data)
			}
			if len(f.Starts)+len(f.Stops)+len(f.LxcUpdates) != 0 {
				t.Errorf("export apply wrote: starts %+v stops %+v updates %+v", f.Starts, f.Stops, f.LxcUpdates)
			}
		})
	}
}

// TestStartOnBootManagesOnboot: startOnBoot alone decides onboot; nil
// leaves it unmanaged, and a missing live key counts as 0.
func TestStartOnBootManagesOnboot(t *testing.T) {
	cases := []struct {
		name       string
		lines      string
		liveOnboot any // nil removes the key
		wantOnboot any // nil means no update carries onboot
	}{
		{"true over live 0 sets 1", "  startOnBoot: true\n", float64(0), 1},
		{"true over live 1 is unchanged", "  startOnBoot: true\n", float64(1), nil},
		{"false over live 1 sets 0", "  startOnBoot: false\n", float64(1), 0},
		{"false over absent key is unchanged", "  startOnBoot: false\n", nil, nil},
		{"undeclared leaves live 1 alone", "", float64(1), nil},
		{"undeclared leaves live 0 alone", "", float64(0), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := seededFake()
			if tc.liveOnboot == nil {
				delete(f.Configs[200], "onboot")
			} else {
				f.Configs[200]["onboot"] = tc.liveOnboot
			}
			res, err := apply(t, f, withPower(tc.lines), resource.DryRunNone)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if got := onbootUpdate(f); got != tc.wantOnboot {
				t.Errorf("onboot update = %#v, want %#v (updates %+v)", got, tc.wantOnboot, f.LxcUpdates)
			}
			wantAction := resource.ActionUnchanged
			if tc.wantOnboot != nil {
				wantAction = resource.ActionConfigured
			}
			if res.Action != wantAction {
				t.Errorf("Action = %v, want %v (diff %+v)", res.Action, wantAction, res.Diff)
			}
		})
	}
}

// TestRunStrategyLeavesOnbootAlone: runStrategy is power only. Paired
// with startOnBoot, each field does its own half.
func TestRunStrategyLeavesOnbootAlone(t *testing.T) {
	cases := []struct {
		name       string
		lines      string
		status     string
		liveOnboot float64
		wantOnboot any
		wantStarts int
		wantStops  int
	}{
		{"Always starts but keeps onboot 0", "  runStrategy: Always\n", "stopped", 0, nil, 1, 0},
		{"Halted stops but keeps onboot 1", "  runStrategy: Halted\n", "running", 1, nil, 0, 1},
		{"Always with startOnBoot false", "  runStrategy: Always\n  startOnBoot: false\n", "stopped", 1, 0, 1, 0},
		{"Halted with startOnBoot true", "  runStrategy: Halted\n  startOnBoot: true\n", "running", 0, 1, 0, 1},
		{"Manual with startOnBoot true", "  runStrategy: Manual\n  startOnBoot: true\n", "stopped", 0, 1, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := seededFake()
			f.GuestList[0].Status = tc.status
			f.Configs[200]["onboot"] = tc.liveOnboot
			if _, err := apply(t, f, withPower(tc.lines), resource.DryRunNone); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if got := onbootUpdate(f); got != tc.wantOnboot {
				t.Errorf("onboot update = %#v, want %#v (updates %+v)", got, tc.wantOnboot, f.LxcUpdates)
			}
			if len(f.Starts) != tc.wantStarts || len(f.Stops) != tc.wantStops {
				t.Errorf("starts %+v stops %+v, want %d / %d", f.Starts, f.Stops, tc.wantStarts, tc.wantStops)
			}
		})
	}
}

// TestCreateOnbootComesFromStartOnBoot: the same rule holds on create.
func TestCreateOnbootComesFromStartOnBoot(t *testing.T) {
	cases := map[string]struct {
		lines string
		want  any // nil means the key is absent
	}{
		"Always alone":      {"  runStrategy: Always\n", nil},
		"Halted alone":      {"  runStrategy: Halted\n", nil},
		"startOnBoot true":  {"  startOnBoot: true\n", 1},
		"startOnBoot false": {"  startOnBoot: false\n", 0},
		"Always + false":    {"  runStrategy: Always\n  startOnBoot: false\n", 0},
		"neither field":     {"", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := templateFake()
			if _, err := apply(t, f, newCT(volidForm+tc.lines), resource.DryRunNone); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			got, has := f.LxcCreates[0].Params["onboot"]
			if tc.want == nil {
				if has {
					t.Errorf("create params onboot = %#v, want absent", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("create params onboot = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// onbootUpdate returns the onboot value of the config PUT, or nil when
// no PUT carried the key.
func onbootUpdate(f *apitest.Fake) any {
	for _, u := range f.LxcUpdates {
		if v, ok := u.Params["onboot"]; ok {
			return v
		}
	}
	return nil
}
