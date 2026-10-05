package lxc

import (
	"sort"
	"strconv"
	"strings"

	"github.com/cyokozai/pvectl/internal/diff"
)

// NormalizeRules builds the diff rules for the given desired params.
// Every rule narrows the live value to the keys and sub-keys the
// manifest wrote (ADR-005): server-generated values (volume names,
// hwaddr, type=veth, ...) and unwritten sub-keys never show up as a
// difference. Volume and NIC keys are dynamic (mp0, net1, ...), so
// rules are derived from the keys actually present.
func NormalizeRules(desired map[string]any) diff.Rules {
	rules := diff.Rules{
		"tags": func(cur, des string) (string, string, bool) {
			return normalizeTags(cur), normalizeTags(des), false
		},
		// Live omits unprivileged for a privileged container.
		keyUnpriv: func(cur, des string) (string, string, bool) {
			if cur == "" {
				cur = "0"
			}
			return cur, des, false
		},
		keyFeatures: normalizeFeaturesRule,
	}
	for key := range desired {
		switch {
		case key == keyRootFS || rxMountKey.MatchString(key):
			rules[key] = normalizeVolumeRule
		case rxNetKey.MatchString(key):
			rules[key] = normalizeNetRule
		}
	}
	return rules
}

// volumeValue is a parsed rootfs / mpN value, either live volume form
// ("storage:vm-200-disk-0,mp=/data,size=32G") or manifest allocation
// form ("storage:32,mp=/data").
type volumeValue struct {
	Storage   string
	Volume    string // empty in allocation form
	SizeBytes int64
	SizeRaw   string // original "size=" value in live form, e.g. "32G"
	Opts      map[string]string
}

func parseVolumeValue(s string) volumeValue {
	v := volumeValue{Opts: map[string]string{}}
	parts := strings.Split(s, ",")

	head := parts[0]
	if storage, rest, ok := strings.Cut(head, ":"); ok {
		v.Storage = storage
		if bytes := allocationSizeBytes(rest); bytes > 0 {
			v.SizeBytes = bytes // allocation form: size in GiB
		} else {
			v.Volume = rest // live form: volume name
		}
	} else {
		v.Volume = head // bind mount: a host path
	}

	for _, part := range parts[1:] {
		k, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if k == "size" {
			v.SizeBytes = parseSizeBytes(val)
			v.SizeRaw = val
			continue
		}
		v.Opts[k] = val
	}
	return v
}

// normalizeVolumeRule compares storage + size + the options the
// manifest writes (mp=); the volume name and other live options
// (backup, mountoptions, ...) are ignored.
//
//nolint:unparam // the bool is fixed by the diff.Rules signature
func normalizeVolumeRule(cur, des string) (string, string, bool) {
	desVol := parseVolumeValue(des)
	if cur == "" {
		return "", canonicalVolume(desVol, nil), false
	}
	managed := keysOf(desVol.Opts)
	return canonicalVolume(parseVolumeValue(cur), managed), canonicalVolume(desVol, managed), false
}

// canonicalVolume renders storage/size plus the managed options (nil
// means all present options) in a stable order.
func canonicalVolume(v volumeValue, managedOpts []string) string {
	if managedOpts == nil {
		managedOpts = keysOf(v.Opts)
	}
	parts := []string{"storage=" + v.Storage, "size=" + strconv.FormatInt(v.SizeBytes, 10)}
	return strings.Join(append(parts, pickOpts(v.Opts, managedOpts)...), ",")
}

// parseOpts parses a "k=v,k=v" value. Keys without "=" are dropped.
func parseOpts(s string) map[string]string {
	opts := map[string]string{}
	if s == "" {
		return opts
	}
	for _, part := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(part, "="); ok {
			opts[k] = v
		}
	}
	return opts
}

// normalizeNetRule compares the NIC sub-keys the manifest writes. The
// generated hwaddr and type=veth are ignored unless written; hwaddr is
// compared case-insensitively.
//
//nolint:unparam // the bool is fixed by the diff.Rules signature
func normalizeNetRule(cur, des string) (string, string, bool) {
	desOpts := upperHWAddr(parseOpts(des))
	if cur == "" {
		return "", strings.Join(pickOpts(desOpts, keysOf(desOpts)), ","), false
	}
	managed := keysOf(desOpts)
	curOpts := upperHWAddr(parseOpts(cur))
	return strings.Join(pickOpts(curOpts, managed), ","), strings.Join(pickOpts(desOpts, managed), ","), false
}

func upperHWAddr(opts map[string]string) map[string]string {
	if mac, ok := opts["hwaddr"]; ok {
		opts["hwaddr"] = strings.ToUpper(mac)
	}
	return opts
}

// normalizeFeaturesRule compares only the written feature flags. A flag
// absent live is off, so it reads as 0.
//
//nolint:unparam // the bool is fixed by the diff.Rules signature
func normalizeFeaturesRule(cur, des string) (string, string, bool) {
	desOpts := parseOpts(des)
	curOpts := parseOpts(cur)
	managed := keysOf(desOpts)
	for _, k := range managed {
		if _, ok := curOpts[k]; !ok {
			curOpts[k] = "0"
		}
	}
	return strings.Join(pickOpts(curOpts, managed), ","), strings.Join(pickOpts(desOpts, managed), ","), false
}

func normalizeTags(s string) string {
	if s == "" {
		return ""
	}
	tags := strings.Split(s, ";")
	sort.Strings(tags)
	return strings.Join(tags, ";")
}

// keysOf returns the map's keys sorted.
func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pickOpts renders "k=v" for each of keys present in opts, sorted.
func pickOpts(opts map[string]string, keys []string) []string {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	var parts []string
	for _, k := range sorted {
		if v, ok := opts[k]; ok {
			parts = append(parts, k+"="+v)
		}
	}
	return parts
}
