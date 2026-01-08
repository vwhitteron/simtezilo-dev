package updater

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version represents a semantic version with optional pre-release suffix.
type Version struct {
	Major      int
	Minor      int
	Patch      int
	PreRelease string
	Raw        string
}

var semverRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([a-zA-Z0-9.-]+))?$`)

// ParseVersion parses a semantic version string.
// Accepts formats: "1.2.3", "v1.2.3", "1.2.3-beta.1".
func ParseVersion(s string) (*Version, error) {
	matches := semverRegex.FindStringSubmatch(s)
	if matches == nil {
		return nil, fmt.Errorf("invalid version format: %s", s)
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	preRelease := ""
	if len(matches) > 4 {
		preRelease = matches[4]
	}

	return &Version{
		Major:      major,
		Minor:      minor,
		Patch:      patch,
		PreRelease: preRelease,
		Raw:        s,
	}, nil
}

// String returns the version as a string in "vX.Y.Z" format.
func (v *Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.PreRelease != "" {
		s += "-" + v.PreRelease
	}

	return s
}

// Compare compares two versions.
// Returns:
//
//	-1 if v < other
//	 0 if v == other
//	 1 if v > other
func (v *Version) Compare(other *Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}

		return 1
	}

	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}

		return 1
	}

	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}

		return 1
	}

	// Pre-release versions have lower precedence than release versions
	// e.g., 1.0.0-alpha < 1.0.0
	if v.PreRelease == "" && other.PreRelease != "" {
		return 1
	}

	if v.PreRelease != "" && other.PreRelease == "" {
		return -1
	}

	if v.PreRelease != "" && other.PreRelease != "" {
		return comparePreRelease(v.PreRelease, other.PreRelease)
	}

	return 0
}

// LessThan returns true if v < other.
func (v *Version) LessThan(other *Version) bool {
	return v.Compare(other) < 0
}

// GreaterThan returns true if v > other.
func (v *Version) GreaterThan(other *Version) bool {
	return v.Compare(other) > 0
}

// Equal returns true if v == other.
func (v *Version) Equal(other *Version) bool {
	return v.Compare(other) == 0
}

// comparePreRelease compares pre-release identifiers.
// Follows semver spec: identifiers are compared left to right.
func comparePreRelease(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	for i := 0; i < len(partsA) && i < len(partsB); i++ {
		// Try numeric comparison first
		numA, errA := strconv.Atoi(partsA[i])
		numB, errB := strconv.Atoi(partsB[i])

		if errA == nil && errB == nil {
			// Both are numeric
			if numA < numB {
				return -1
			}

			if numA > numB {
				return 1
			}
		} else if errA == nil {
			// Numeric identifiers have lower precedence than alphanumeric
			return -1
		} else if errB == nil {
			return 1
		} else {
			// Both are alphanumeric, compare as strings
			cmp := strings.Compare(partsA[i], partsB[i])
			if cmp != 0 {
				return cmp
			}
		}
	}

	// Fewer identifiers = lower precedence
	if len(partsA) < len(partsB) {
		return -1
	}

	if len(partsA) > len(partsB) {
		return 1
	}

	return 0
}
