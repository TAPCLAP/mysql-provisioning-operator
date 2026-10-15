package mysql

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	identPattern   = regexp.MustCompile(`^[A-Za-z0-9_$-]+$`)
	hostPattern    = regexp.MustCompile(`^[A-Za-z0-9_.:%-]+$`)
	charsetPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
)

// QuoteIdent returns a backtick-quoted MySQL identifier.
func QuoteIdent(name string) (string, error) {
	if !identPattern.MatchString(name) {
		return "", fmt.Errorf("invalid identifier %q", name)
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`", nil
}

// QuoteLiteral returns a single-quoted MySQL string literal.
func QuoteLiteral(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('\'')
	for _, r := range value {
		switch r {
		case '\\', '\'':
			b.WriteByte('\\')
			b.WriteRune(r)
		case 0:
			b.WriteString(`\0`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// ValidIdent reports whether name is a safe unquoted identifier of the given max length.
func ValidIdent(name string, maxLen int) bool {
	return name != "" && len(name) <= maxLen && identPattern.MatchString(name)
}

// ValidHost reports whether host is a safe MySQL account host.
func ValidHost(host string) bool {
	return host != "" && len(host) <= 255 && hostPattern.MatchString(host)
}

// ValidCharset reports whether a character set or collation name is safe to inline.
func ValidCharset(name string) bool {
	return name != "" && len(name) <= 64 && charsetPattern.MatchString(name)
}
