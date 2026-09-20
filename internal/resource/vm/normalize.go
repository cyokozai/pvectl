package vm

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cyokozai/pvectl/internal/diff"
)

var (
	rxDiskKey = regexp.MustCompile(`^(scsi|virtio|sata|ide)\d+$`)
	rxNetKey  = regexp.MustCompile(`^net\d+$`)
	// rxSize matches "32G", "512M", "1.5T", or a bare GB number.
	rxSize = regexp.MustCompile(`^([0-9]*\.?[0-9]+)([KMGT]?)$`)
)

// NormalizeRules builds the diff rules for the given desired params.
// Disk and NIC keys are dynamic (scsi0, net1, ...), so rules are
// derived from the keys actually present.
func NormalizeRules(desired map[string]any) diff.Rules {
	rules := diff.Rules{
		// The API masks cipassword, so it can only ever be set, not diffed.
		"cipassword": func(_, _ string) (string, string, bool) { return "", "", true },
		"tags": func(cur, des string) (string, string, bool) {
			return normalizeTags(cur), normalizeTags(des), false
		},
		"sshkeys": func(cur, des string) (string, string, bool) {
			return decodeSSHKeys(cur), decodeSSHKeys(des), false
		},
		"cpu": func(cur, des string) (string, string, bool) {
			return normalizeCPUType(cur), normalizeCPUType(des), false
		},
	}
	for key := range desired {
		switch {
		case rxDiskKey.MatchString(key):
			rules[key] = normalizeDiskRule
		case rxNetKey.MatchString(key):
			rules[key] = normalizeNetRule
		}
	}
	return rules
}

// diskValue is a parsed disk config value, either live volume form
// ("storage:vm-100-disk-0,size=32G,...") or manifest allocation form
// ("storage:32,...").
type diskValue struct {
	Storage   string
	Volume    string // empty in allocation form
	SizeBytes int64
	SizeRaw   string // original "size=" value in live form, e.g. "32G"
	Opts      map[string]string
}

func parseDiskValue(s string) diskValue {
	d := diskValue{Opts: map[string]string{}}
	parts := strings.Split(s, ",")

	head := parts[0]
	if idx := strings.IndexByte(head, ':'); idx >= 0 {
		d.Storage = head[:idx]
		rest := head[idx+1:]
		if bytes := allocationSizeBytes(rest); bytes > 0 {
			d.SizeBytes = bytes // allocation form: size in GB
		} else {
			d.Volume = rest // live form: volume name
		}
	} else {
		d.Volume = head
	}

	for _, part := range parts[1:] {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if k == "size" {
			d.SizeBytes = parseSizeBytes(v)
			d.SizeRaw = v
			continue
		}
		d.Opts[k] = v
	}
	return d
}

// normalizeDiskRule compares storage + size + the options the manifest
// explicitly sets; the server-assigned volume name and any extra live
// options are ignored.
//
//nolint:unparam // the bool is fixed by the diff.Rules signature
func normalizeDiskRule(cur, des string) (string, string, bool) {
	curDisk, desDisk := parseDiskValue(cur), parseDiskValue(des)
	if cur == "" {
		return "", canonicalDisk(desDisk, nil), false
	}
	managed := make([]string, 0, len(desDisk.Opts))
	for k := range desDisk.Opts {
		managed = append(managed, k)
	}
	return canonicalDisk(curDisk, managed), canonicalDisk(desDisk, managed), false
}

// canonicalDisk renders storage/size plus the managed options (nil
// means all present options) in a stable order.
func canonicalDisk(d diskValue, managedOpts []string) string {
	parts := []string{"storage=" + d.Storage, "size=" + strconv.FormatInt(d.SizeBytes, 10)}
	keys := managedOpts
	if keys == nil {
		keys = make([]string, 0, len(d.Opts))
		for k := range d.Opts {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v, ok := d.Opts[k]; ok {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, ",")
}

// netValue is a parsed NIC config value.
type netValue struct {
	Model string
	MAC   string
	Opts  map[string]string
}

func parseNetValue(s string) netValue {
	n := netValue{Opts: map[string]string{}}
	for i, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, "=")
		if i == 0 {
			// "virtio" (manifest) or "virtio=MAC" (live)
			n.Model = k
			if ok {
				n.MAC = v
			}
			continue
		}
		if ok {
			n.Opts[k] = v
		}
	}
	return n
}

// normalizeNetRule compares model + the options the manifest sets. The
// server-generated MAC is ignored unless the manifest pins one.
//
//nolint:unparam // the bool is fixed by the diff.Rules signature
func normalizeNetRule(cur, des string) (string, string, bool) {
	curNet, desNet := parseNetValue(cur), parseNetValue(des)
	if cur == "" {
		return "", canonicalNet(desNet, nil, desNet.MAC != ""), false
	}
	managed := make([]string, 0, len(desNet.Opts))
	for k := range desNet.Opts {
		managed = append(managed, k)
	}
	compareMAC := desNet.MAC != ""
	return canonicalNet(curNet, managed, compareMAC), canonicalNet(desNet, managed, compareMAC), false
}

func canonicalNet(n netValue, managedOpts []string, includeMAC bool) string {
	parts := []string{"model=" + n.Model}
	if includeMAC {
		parts = append(parts, "mac="+strings.ToUpper(n.MAC))
	}
	keys := managedOpts
	if keys == nil {
		keys = make([]string, 0, len(n.Opts))
		for k := range n.Opts {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v, ok := n.Opts[k]; ok {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, ",")
}

func normalizeTags(s string) string {
	if s == "" {
		return ""
	}
	tags := strings.Split(s, ";")
	sort.Strings(tags)
	return strings.Join(tags, ";")
}

// decodeSSHKeys unescapes both %XX and + encodings so equivalent
// encodings compare equal.
func decodeSSHKeys(s string) string {
	if decoded, err := url.QueryUnescape(strings.ReplaceAll(s, "+", "%20")); err == nil {
		return decoded
	}
	return s
}

// normalizeCPUType extracts the base CPU type from values like
// "cputype=host,flags=+aes".
func normalizeCPUType(s string) string {
	first := strings.Split(s, ",")[0]
	return strings.TrimPrefix(first, "cputype=")
}

// parseSizeBytes converts "32G"/"512M"/"1.5T"/"32" (GB) to bytes.
// Returns 0 when the string is not a size.
func parseSizeBytes(s string) int64 {
	m := rxSize.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil {
		return 0
	}
	value, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	unit := m[2]
	var mult float64
	switch unit {
	case "K":
		mult = 1 << 10
	case "M":
		mult = 1 << 20
	case "T":
		mult = 1 << 40
	default: // "G" or bare number (GB)
		mult = 1 << 30
	}
	return int64(value * mult)
}

// allocationSizeBytes interprets the part after ":" in allocation form
// ("32", "0.5") as GB. Volume names ("vm-100-disk-0", "cloudinit")
// return 0.
func allocationSizeBytes(s string) int64 {
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return 0
	}
	return parseSizeBytes(s)
}
