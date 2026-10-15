package validate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

const maxPatternLength = 256

var deniedPrivileges = map[string]struct{}{
	"SUPER":            {},
	"FILE":             {},
	"SHUTDOWN":         {},
	"RELOAD":           {},
	"BINLOG_ADMIN":     {},
	"SYSTEM_USER":      {},
	"CONNECTION_ADMIN": {},
	"GRANT_OPTION":     {},
	"CREATE_USER":      {},
	"ALL":              {},
	"ALL_PRIVILEGES":   {},
}

var systemSchemas = map[string]struct{}{
	"mysql":              {},
	"sys":                {},
	"information_schema": {},
	"performance_schema": {},
}

// NormalizePrivilege uppercases a privilege and turns spaces into underscores.
func NormalizePrivilege(priv string) string {
	priv = strings.ToUpper(strings.TrimSpace(priv))
	return strings.ReplaceAll(priv, " ", "_")
}

// PrivilegeDenied reports a platform-wide forbidden privilege.
func PrivilegeDenied(priv string) bool {
	_, ok := deniedPrivileges[NormalizePrivilege(priv)]
	return ok
}

// SchemaDenied reports a schema the operator will not create or grant.
func SchemaDenied(name string) bool {
	_, ok := systemSchemas[strings.ToLower(name)]
	return ok
}

// Match reports whether name matches the pattern rule.
func Match(rule mysqlv1alpha1.PatternRule, name string) (bool, error) {
	for _, pattern := range rule.Patterns {
		if len(pattern) > maxPatternLength {
			return false, fmt.Errorf("pattern exceeds %d characters", maxPatternLength)
		}
		switch rule.NormalizedType() {
		case mysqlv1alpha1.PatternRegex:
			re, err := regexp.Compile(`\A(?:` + pattern + `)\z`)
			if err != nil {
				return false, fmt.Errorf("compile regex %q: %w", pattern, err)
			}
			if re.MatchString(name) {
				return true, nil
			}
		default:
			ok, err := filepath.Match(pattern, name)
			if err != nil {
				return false, fmt.Errorf("compile glob %q: %w", pattern, err)
			}
			if ok {
				return true, nil
			}
		}
	}
	return false, nil
}

// ParseRetention returns the retention window, applying the default and the 5m floor.
func ParseRetention(raw string) (time.Duration, error) {
	if raw == "" {
		raw = mysqlv1alpha1.DefaultPasswordRetention
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse retention: %w", err)
	}
	minRetention, err := time.ParseDuration(mysqlv1alpha1.MinPasswordRetention)
	if err != nil {
		return 0, err
	}
	if d < minRetention {
		return 0, fmt.Errorf("retention must be at least %s", mysqlv1alpha1.MinPasswordRetention)
	}
	return d, nil
}

// ParseRotateEvery returns the schedule interval. Empty means manual rotation only.
func ParseRotateEvery(raw string, retention time.Duration) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	every, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse rotateEvery: %w", err)
	}
	if every <= retention {
		return 0, fmt.Errorf("rotateEvery must be longer than retention")
	}
	return every, nil
}
