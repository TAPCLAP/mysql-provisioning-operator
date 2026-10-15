//go:build integration

package mysql_test

import (
	"context"
	"testing"
	"time"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysqltest"
)

func TestMySQLProvisionOverTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	server := mysqltest.Start(t, ctx)
	db := server.Open(t, ctx, server.Admin)
	defer func() { _ = db.Close() }()

	if cipher := statusValue(t, ctx, db, "Ssl_cipher"); cipher == "" {
		t.Fatal("operator session did not negotiate TLS")
	}

	state, err := mysql.EnsureDatabase(ctx, db, "payments_main", "utf8mb3", "utf8mb3_general_ci")
	if err != nil {
		t.Fatal(err)
	}
	if !sameCharset(state.Charset, "utf8mb3") || state.Collation != "utf8mb3_general_ci" {
		t.Fatalf("created charset=%s collation=%s", state.Charset, state.Collation)
	}
	again, err := mysql.EnsureDatabase(ctx, db, "payments_main", "utf8mb4", "utf8mb4_0900_ai_ci")
	if err != nil {
		t.Fatal(err)
	}
	if !sameCharset(again.Charset, "utf8mb3") || again.Collation != "utf8mb3_general_ci" {
		t.Fatalf("existing database was altered: charset=%s collation=%s", again.Charset, again.Collation)
	}

	const appPassword = "App-secret-1"
	if err := mysql.ApplyAccount(ctx, db, mysql.Account{
		User: "payments_app", Host: "%", Plugin: mysqlv1alpha1.AuthPluginCachingSHA2,
		Password: appPassword, ApplyPassword: true, RequireTLS: true,
		Grants: []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SELECT"}}},
	}); err != nil {
		t.Fatal(err)
	}

	plain := server.Admin
	plain.User = "payments_app"
	plain.Password = appPassword
	plain.TLS = nil
	plain.CA = nil
	if _, err := server.TryOpen(ctx, plain); err == nil {
		t.Fatal("REQUIRE SSL account connected without TLS")
	}

	app := server.Admin
	app.User = "payments_app"
	app.Password = appPassword
	appDB := server.Open(t, ctx, app)
	defer func() { _ = appDB.Close() }()
	if cipher := statusValue(t, ctx, appDB, "Ssl_cipher"); cipher == "" {
		t.Fatal("application session did not negotiate TLS")
	}
	if !schemaExists(t, ctx, db, "payments_main") {
		t.Fatal("database disappeared")
	}
}

func statusValue(t *testing.T, ctx context.Context, db mysql.DB, name string) string {
	t.Helper()
	rows, err := db.Query(ctx, "SHOW STATUS LIKE "+mysql.QuoteLiteral(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatalf("status %s has no row: %v", name, rows.Err())
	}
	var got, value string
	if err := rows.Scan(&got, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func schemaExists(t *testing.T, ctx context.Context, db mysql.DB, name string) bool {
	t.Helper()
	rows, err := db.Query(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = "+mysql.QuoteLiteral(name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	return rows.Next()
}

func sameCharset(got, want string) bool {
	if got == "utf8" {
		got = "utf8mb3"
	}
	if want == "utf8" {
		want = "utf8mb3"
	}
	return got == want
}
