package utils

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSize parses a human-readable size string into bytes.
// Supported formats:
//   "50MB"  -> 50 * 1000 * 1000
//   "50MiB" -> 50 * 1024 * 1024
//   "1GB"   -> 1 * 1000 * 1000 * 1000
//   "500"   -> 500 (bare number = bytes)
// Returns error on unknown units.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// Try bare number.
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v, nil
	}

	// Find the boundary between digits and unit.
	lastDigit := -1
	for i, c := range s {
		if c >= '0' && c <= '9' || c == '.' {
			lastDigit = i
		} else {
			break
		}
	}
	if lastDigit < 0 {
		return 0, fmt.Errorf("no numeric value in %q", s)
	}

	numPart := s[:lastDigit+1]
	unitPart := strings.TrimSpace(s[lastDigit+1:])

	val, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", numPart, err)
	}

	// Map units to multipliers.
	unitPart = strings.ToLower(unitPart)
	var multiplier float64
	switch unitPart {
	case "b", "":
		multiplier = 1
	case "kb", "k":
		multiplier = 1000
	case "kib", "ki":
		multiplier = 1024
	case "mb", "m":
		multiplier = 1000 * 1000
	case "mib", "mi":
		multiplier = 1024 * 1024
	case "gb", "g":
		multiplier = 1000 * 1000 * 1000
	case "gib", "gi":
		multiplier = 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size unit %q (use KB, MB, GB or KiB, MiB, GiB)", unitPart)
	}

	return int64(val * multiplier), nil
}
