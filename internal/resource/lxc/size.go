package lxc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// rxSize matches "32G", "512M", "1.5T", or a bare GiB number.
var rxSize = regexp.MustCompile(`^([0-9]*\.?[0-9]+)([KMGT]?)$`)

// parseSizeBytes converts "32G"/"512M"/"1.5T"/"32" (GiB) to bytes.
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
	var mult float64
	switch m[2] {
	case "K":
		mult = 1 << 10
	case "M":
		mult = 1 << 20
	case "T":
		mult = 1 << 40
	default: // "G" or bare number (GiB)
		mult = 1 << 30
	}
	return int64(value * mult)
}

// allocationSizeBytes interprets the part after ":" in allocation form
// ("8", "0.5") as GiB. Volume names ("vm-200-disk-0") return 0.
func allocationSizeBytes(s string) int64 {
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return 0
	}
	return parseSizeBytes(s)
}

// sizeToGBString converts a size like "8G" or "512M" to the GiB number
// Proxmox allocation syntax expects ("8", "0.5").
func sizeToGBString(size string) string {
	bytes := parseSizeBytes(size)
	if bytes <= 0 {
		return size // let the API reject malformed sizes with its own error
	}
	return fmt.Sprintf("%g", float64(bytes)/(1<<30))
}

// bytesToSizeString renders bytes the way manifests write sizes.
func bytesToSizeString(bytes int64) string {
	switch {
	case bytes <= 0:
		return ""
	case bytes%(1<<30) == 0:
		return fmt.Sprintf("%dG", bytes/(1<<30))
	case bytes%(1<<20) == 0:
		return fmt.Sprintf("%dM", bytes/(1<<20))
	default:
		return fmt.Sprintf("%dK", bytes/(1<<10))
	}
}
