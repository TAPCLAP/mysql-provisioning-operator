package mysql

import (
	"strconv"
	"strings"
)

// SupportsDualPassword reports whether VERSION() can RETAIN CURRENT PASSWORD.
// MySQL gained the second password in 8.0.14.
func SupportsDualPassword(version string) bool {
	major, minor, patch := parseVersion(version)
	if major > 8 {
		return true
	}
	if major < 8 {
		return false
	}
	if minor > 0 {
		return true
	}
	return patch >= 14
}

func parseVersion(version string) (major, minor, patch int) {
	version = strings.TrimSpace(version)
	if cut, _, ok := strings.Cut(version, "-"); ok {
		version = cut
	}
	parts := strings.Split(version, ".")
	if len(parts) > 0 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return major, minor, patch
}
