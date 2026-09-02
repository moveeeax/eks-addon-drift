// Package version compares EKS managed add-on version strings.
//
// Add-on versions look like "v1.11.1-eksbuild.9" or "v1.29.0-eksbuild.1":
// a leading "v", a semver-ish core, and an "-eksbuild.N" build counter. AWS
// bumps the build counter far more often than the core, so a correct compare
// has to order by core first and then by the build number.
package version

import (
	"strconv"
	"strings"
)

type parsed struct {
	core  [3]int
	build int
	ok    bool
}

func parse(v string) parsed {
	var p parsed
	s := strings.TrimSpace(v)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if s == "" {
		return p
	}

	// Split the core (x.y.z) from any pre-release / build suffix.
	core := s
	suffix := ""
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		core = s[:i]
		suffix = s[i+1:]
	}

	parts := strings.Split(core, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return p
		}
		p.core[i] = n
	}

	// Pull the numeric build counter out of "eksbuild.N" (or any trailing number).
	if suffix != "" {
		if idx := strings.LastIndex(suffix, "."); idx >= 0 {
			if n, err := strconv.Atoi(suffix[idx+1:]); err == nil {
				p.build = n
			}
		} else if n, err := strconv.Atoi(suffix); err == nil {
			p.build = n
		}
	}

	p.ok = true
	return p
}

// Compare returns -1 if a < b, 0 if equal, and +1 if a > b. Unparseable
// versions sort before parseable ones so genuine versions always win the
// "latest" comparison.
func Compare(a, b string) int {
	pa, pb := parse(a), parse(b)
	if !pa.ok || !pb.ok {
		switch {
		case pa.ok == pb.ok:
			return strings.Compare(a, b)
		case pa.ok:
			return 1
		default:
			return -1
		}
	}
	for i := 0; i < 3; i++ {
		if pa.core[i] != pb.core[i] {
			if pa.core[i] < pb.core[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case pa.build < pb.build:
		return -1
	case pa.build > pb.build:
		return 1
	default:
		return 0
	}
}

// Max returns the highest version in vs, or "" when vs is empty.
func Max(vs []string) string {
	best := ""
	for i, v := range vs {
		if i == 0 || Compare(v, best) > 0 {
			best = v
		}
	}
	return best
}
