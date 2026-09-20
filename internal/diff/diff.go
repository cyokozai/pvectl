// Package diff computes managed-key diffs between a live Proxmox VE
// config and the flat parameter map a manifest converts to. Only keys
// present in the desired map are compared; everything the server adds
// on its own (vmgenid, digest, ...) is ignored.
package diff

import (
	"fmt"
	"sort"
	"strconv"
)

// Rules maps a config key to a normalizer applied before comparison.
// Returning skip=true excludes the key from the diff entirely
// (write-only keys such as cipassword).
type Rules map[string]func(current, desired string) (normCur, normDes string, skip bool)

// Entry is a single differing key.
type Entry struct {
	Key     string
	Current string // canonical live value; "" when the key is absent
	Desired string
}

// Result is the set of differing keys, sorted by key.
type Result struct {
	Entries []Entry
}

// Empty reports whether live and desired state agree on all managed keys.
func (r Result) Empty() bool { return len(r.Entries) == 0 }

// Compute compares desired against current, key by key.
func Compute(current, desired map[string]any, rules Rules) Result {
	var entries []Entry
	for key, want := range desired {
		curStr := ""
		if cur, ok := current[key]; ok {
			curStr = canonical(cur)
		}
		desStr := canonical(want)

		if rule, ok := rules[key]; ok {
			var skip bool
			curStr, desStr, skip = rule(curStr, desStr)
			if skip {
				continue
			}
		}
		if curStr != desStr {
			entries = append(entries, Entry{Key: key, Current: curStr, Desired: desStr})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return Result{Entries: entries}
}

// canonical renders any JSON/YAML scalar the way Proxmox VE writes it:
// numbers without exponent noise, booleans as 0/1.
func canonical(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "1"
		}
		return "0"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}
