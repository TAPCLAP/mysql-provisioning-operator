package mysql

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

// DesiredGrant is one privilege set the operator should converge.
type DesiredGrant struct {
	Database   string
	Table      string
	Privileges []string
}

type grantKey struct {
	database string
	table    string
}

// ParseGrants extracts database and table privileges from SHOW GRANTS output.
// USAGE on *.* is ignored. WITH GRANT OPTION is reported as GRANT_OPTION.
func ParseGrants(lines []string) (map[grantKey]map[string]struct{}, error) {
	parsed := map[grantKey]map[string]struct{}{}
	for _, line := range lines {
		privs, object, grantOption, ok := parseGrantLine(line)
		if !ok {
			continue
		}
		db, table, global := splitGrantObject(object)
		if global {
			continue
		}
		key := grantKey{database: db, table: table}
		set := parsed[key]
		if set == nil {
			set = map[string]struct{}{}
			parsed[key] = set
		}
		for _, priv := range privs {
			set[normalizePriv(priv)] = struct{}{}
		}
		if grantOption {
			set["GRANT_OPTION"] = struct{}{}
		}
	}
	return parsed, nil
}

var grantLine = regexp.MustCompile(`(?i)^GRANT\s+(.+?)\s+ON\s+(\S+)\s+TO\s+`)

func parseGrantLine(line string) (privs []string, object string, grantOption bool, ok bool) {
	match := grantLine.FindStringSubmatch(strings.TrimSpace(line))
	if match == nil {
		return nil, "", false, false
	}
	rawPrivs := match[1]
	object = match[2]
	if strings.Contains(strings.ToUpper(line), "WITH GRANT OPTION") {
		grantOption = true
	}
	for part := range strings.SplitSeq(rawPrivs, ",") {
		priv := strings.TrimSpace(part)
		if priv == "" {
			continue
		}
		privs = append(privs, priv)
	}
	return privs, object, grantOption, true
}

func splitGrantObject(object string) (db string, table string, global bool) {
	object = strings.TrimSpace(object)
	dbPart, tablePart, ok := splitOnce(object, ".")
	if !ok {
		return "", "", true
	}
	db = unquoteIdent(dbPart)
	table = unquoteIdent(tablePart)
	if db == "*" {
		return "", "", true
	}
	return db, table, false
}

func splitOnce(value, sep string) (string, string, bool) {
	before, after, ok := strings.Cut(value, sep)
	if !ok {
		return "", "", false
	}
	return before, after, true
}

func unquoteIdent(value string) string {
	value = strings.Trim(value, "`")
	return strings.ReplaceAll(value, "``", "`")
}

func normalizePriv(priv string) string {
	priv = strings.ToUpper(strings.TrimSpace(priv))
	priv = strings.ReplaceAll(priv, " ", "_")
	if priv == "ALL_PRIVILEGES" {
		return "ALL"
	}
	return priv
}

// GrantStatements returns GRANT and REVOKE statements that move actual to desired.
func GrantStatements(user, host string, desired []DesiredGrant, actual map[grantKey]map[string]struct{}) ([]string, error) {
	account := QuoteLiteral(user) + "@" + QuoteLiteral(host)
	want := map[grantKey]map[string]struct{}{}
	for _, grant := range desired {
		table := grant.Table
		if table == "" {
			table = "*"
		}
		key := grantKey{database: grant.Database, table: table}
		set := want[key]
		if set == nil {
			set = map[string]struct{}{}
			want[key] = set
		}
		for _, priv := range grant.Privileges {
			set[normalizePriv(priv)] = struct{}{}
		}
	}
	union := map[grantKey]map[string]struct{}{}
	for key, privs := range want {
		union[key] = privs
	}
	for key := range actual {
		if _, ok := union[key]; !ok {
			union[key] = nil
		}
	}
	var revokeAll, grants, revokes []string
	for _, key := range sortedGrantKeys(union) {
		have := actual[key]
		need := want[key]
		_, haveAll := have["ALL"]
		_, wantAll := need["ALL"]
		if haveAll && !wantAll {
			stmt, err := privilegeStatement("REVOKE", key, []string{"ALL"}, account)
			if err != nil {
				return nil, err
			}
			revokeAll = append(revokeAll, stmt)
			have = nil
		}
		if wantAll {
			stmt, err := privilegeStatement("GRANT", key, difference(map[string]struct{}{"ALL": {}}, have), account)
			if err != nil {
				return nil, err
			}
			if stmt != "" {
				grants = append(grants, stmt)
			}
			continue
		}
		stmt, err := privilegeStatement("GRANT", key, difference(need, have), account)
		if err != nil {
			return nil, err
		}
		if stmt != "" {
			grants = append(grants, stmt)
		}
		if haveAll {
			continue
		}
		stmt, err = privilegeStatement("REVOKE", key, difference(have, need), account)
		if err != nil {
			return nil, err
		}
		if stmt != "" {
			revokes = append(revokes, stmt)
		}
	}
	statements := append(revokeAll, grants...)
	return append(statements, revokes...), nil
}

func sortedGrantKeys(grants map[grantKey]map[string]struct{}) []grantKey {
	keys := make([]grantKey, 0, len(grants))
	for key := range grants {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].database != keys[j].database {
			return keys[i].database < keys[j].database
		}
		return keys[i].table < keys[j].table
	})
	return keys
}

func difference(left, right map[string]struct{}) []string {
	out := make([]string, 0, len(left))
	for priv := range left {
		if _, ok := right[priv]; !ok {
			out = append(out, strings.ReplaceAll(priv, "_", " "))
		}
	}
	sort.Strings(out)
	return out
}

func privilegeStatement(verb string, key grantKey, privs []string, account string) (string, error) {
	if len(privs) == 0 {
		return "", nil
	}
	db, err := QuoteIdent(key.database)
	if err != nil {
		return "", err
	}
	var table string
	if key.table == "*" {
		table = "*"
	} else {
		table, err = QuoteIdent(key.table)
		if err != nil {
			return "", err
		}
	}
	target := "TO"
	if verb == "REVOKE" {
		target = "FROM"
	}
	return fmt.Sprintf("%s %s ON %s.%s %s %s", verb, strings.Join(privs, ", "), db, table, target, account), nil
}

// ExpandGrants turns spec grants into one DesiredGrant per table.
func ExpandGrants(grants []mysqlv1alpha1.GrantSpec) []DesiredGrant {
	var out []DesiredGrant
	for _, grant := range grants {
		tables := grant.Tables
		if len(tables) == 0 {
			tables = []string{"*"}
		}
		for _, table := range tables {
			if table == "" {
				table = "*"
			}
			out = append(out, DesiredGrant{
				Database:   grant.Database,
				Table:      table,
				Privileges: append([]string(nil), grant.Privileges...),
			})
		}
	}
	return out
}
