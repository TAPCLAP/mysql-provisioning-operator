package mysql

import (
	"context"
	"fmt"
	"strings"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

// Account is the desired state of one user@host pair.
type Account struct {
	User               string
	Host               string
	Plugin             mysqlv1alpha1.AuthPlugin
	Password           string
	ApplyPassword      bool
	Retain             bool
	RequireTLS         bool
	DiscardOld         bool
	MaxUserConnections *int32
	MaxQueriesPerHour  *int32
	Grants             []mysqlv1alpha1.GrantSpec
}

// ApplyAccount creates or updates one MySQL account and its grants.
func ApplyAccount(ctx context.Context, db DB, account Account) error {
	if !ValidIdent(account.User, 32) {
		return fmt.Errorf("invalid username %q", account.User)
	}
	if !ValidHost(account.Host) {
		return fmt.Errorf("invalid host %q", account.Host)
	}
	if account.Plugin != mysqlv1alpha1.AuthPluginCachingSHA2 && account.Plugin != mysqlv1alpha1.AuthPluginNative {
		return fmt.Errorf("unsupported auth plugin %q", account.Plugin)
	}
	exists, currentPlugin, err := lookupAccount(ctx, db, account.User, account.Host)
	if err != nil {
		return err
	}
	if account.DiscardOld && exists {
		if err := exec(ctx, db, "ALTER USER "+accountLiteral(account)+" DISCARD OLD PASSWORD"); err != nil {
			return err
		}
	}
	if !exists {
		if err := exec(ctx, db, createUserSQL(account)); err != nil {
			return err
		}
	} else if account.ApplyPassword || currentPlugin != string(account.Plugin) {
		if err := exec(ctx, db, alterUserSQL(account)); err != nil {
			return err
		}
	}
	if err := exec(ctx, db, requireSQL(account)); err != nil {
		return err
	}
	if limit := limitSQL(account); limit != "" {
		if err := exec(ctx, db, limit); err != nil {
			return err
		}
	}
	return syncGrants(ctx, db, account)
}

// DropAccount removes one user@host pair.
func DropAccount(ctx context.Context, db DB, user, host string) error {
	if !ValidIdent(user, 32) || !ValidHost(host) {
		return fmt.Errorf("invalid account %q@%q", user, host)
	}
	return exec(ctx, db, "DROP USER IF EXISTS "+QuoteLiteral(user)+"@"+QuoteLiteral(host))
}

// DatabaseExists reports whether schema is present.
func DatabaseExists(ctx context.Context, db DB, name string) (bool, error) {
	state, err := readDatabase(ctx, db, name)
	if err != nil {
		return false, err
	}
	return state.Exists, nil
}

func lookupAccount(ctx context.Context, db DB, user, host string) (bool, string, error) {
	query := "SELECT plugin FROM mysql.user WHERE user = " + QuoteLiteral(user) + " AND host = " + QuoteLiteral(host)
	rows, err := db.Query(ctx, query)
	if err != nil {
		return false, "", err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, "", rows.Err()
	}
	var plugin string
	if err := rows.Scan(&plugin); err != nil {
		return false, "", err
	}
	return true, plugin, rows.Err()
}

func accountLiteral(account Account) string {
	return QuoteLiteral(account.User) + "@" + QuoteLiteral(account.Host)
}

func createUserSQL(account Account) string {
	return fmt.Sprintf(
		"CREATE USER IF NOT EXISTS %s IDENTIFIED WITH %s BY %s %s",
		accountLiteral(account),
		account.Plugin,
		QuoteLiteral(account.Password),
		tlsClause(account.RequireTLS),
	)
}

func alterUserSQL(account Account) string {
	retain := ""
	if account.Retain {
		retain = " RETAIN CURRENT PASSWORD"
	}
	return fmt.Sprintf(
		"ALTER USER %s IDENTIFIED WITH %s BY %s%s",
		accountLiteral(account),
		account.Plugin,
		QuoteLiteral(account.Password),
		retain,
	)
}

func requireSQL(account Account) string {
	return "ALTER USER " + accountLiteral(account) + " " + tlsClause(account.RequireTLS)
}

func tlsClause(require bool) string {
	if require {
		return "REQUIRE SSL"
	}
	return "REQUIRE NONE"
}

func limitSQL(account Account) string {
	var parts []string
	if account.MaxUserConnections != nil {
		parts = append(parts, fmt.Sprintf("MAX_USER_CONNECTIONS %d", *account.MaxUserConnections))
	}
	if account.MaxQueriesPerHour != nil {
		parts = append(parts, fmt.Sprintf("MAX_QUERIES_PER_HOUR %d", *account.MaxQueriesPerHour))
	}
	if len(parts) == 0 {
		return ""
	}
	return "ALTER USER " + accountLiteral(account) + " WITH " + strings.Join(parts, " ")
}

func syncGrants(ctx context.Context, db DB, account Account) error {
	for _, grant := range account.Grants {
		exists, err := DatabaseExists(ctx, db, grant.Database)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("database %s does not exist yet", grant.Database)
		}
	}
	lines, err := showGrants(ctx, db, account.User, account.Host)
	if err != nil {
		return err
	}
	actual, err := ParseGrants(lines)
	if err != nil {
		return err
	}
	statements, err := GrantStatements(account.User, account.Host, ExpandGrants(account.Grants), actual)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if err := exec(ctx, db, statement); err != nil {
			return err
		}
	}
	return nil
}

func showGrants(ctx context.Context, db DB, user, host string) ([]string, error) {
	rows, err := db.Query(ctx, "SHOW GRANTS FOR "+QuoteLiteral(user)+"@"+QuoteLiteral(host))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

// ServerVersion reads SELECT VERSION().
func ServerVersion(ctx context.Context, db DB) (string, error) {
	rows, err := db.Query(ctx, "SELECT VERSION()")
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("empty version")
	}
	var version string
	if err := rows.Scan(&version); err != nil {
		return "", err
	}
	return version, rows.Err()
}
