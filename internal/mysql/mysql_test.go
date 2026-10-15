package mysql_test

import (
	"context"
	"strings"
	"testing"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysqltest"
)

func TestSelectMasterDetected(t *testing.T) {
	cluster := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{
		Members: []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}, {Name: "db1-2", Host: "db1-2"}},
		Master:  mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
	}}
	probes := map[string]mysql.Probe{
		"db1-1": {Reachable: true, ReadOnly: false, SuperReadOnly: false},
		"db1-2": {Reachable: true, ReadOnly: true, SuperReadOnly: true, ReplicationConfigured: true},
	}
	sel := mysql.SelectMaster(cluster, probes)
	if sel.Master == nil || sel.Master.Name != "db1-1" {
		t.Fatalf("master = %#v", sel.Master)
	}
	if sel.NoMaster || sel.SplitBrain {
		t.Fatalf("unexpected selection %#v", sel)
	}
}

func TestSelectMasterRequiresNoReplication(t *testing.T) {
	cluster := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{
		Members: []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1"}},
		Master:  mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
	}}
	probes := map[string]mysql.Probe{"db1-1": {Reachable: true, ReplicationConfigured: true}}
	sel := mysql.SelectMaster(cluster, probes)
	if !sel.NoMaster || sel.Master != nil {
		t.Fatalf("replica was selected: %#v", sel)
	}
}

func TestSelectMasterExplicitIgnoresCriteria(t *testing.T) {
	cluster := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{
		Members: []mysqlv1alpha1.MySQLMember{{Name: "db1-1", Host: "db1-1", Master: true}, {Name: "db1-2", Host: "db1-2"}},
		Master:  mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeExplicit},
	}}
	probes := map[string]mysql.Probe{
		"db1-1": {Reachable: true, ReadOnly: true, ReplicationConfigured: true},
		"db1-2": {Reachable: true},
	}
	sel := mysql.SelectMaster(cluster, probes)
	if sel.Master == nil || sel.Master.Name != "db1-1" {
		t.Fatalf("explicit master = %#v", sel.Master)
	}
	if sel.WouldPass == nil || *sel.WouldPass {
		t.Fatalf("explicit master should not pass criteria: %#v", sel.WouldPass)
	}
}

func TestSelectMasterSplitBrain(t *testing.T) {
	cluster := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{
		Members: []mysqlv1alpha1.MySQLMember{{Name: "a", Host: "a"}, {Name: "b", Host: "b"}},
		Master:  mysqlv1alpha1.MasterSelection{Mode: mysqlv1alpha1.MasterModeDetected},
	}}
	probes := map[string]mysql.Probe{"a": {Reachable: true}, "b": {Reachable: true}}
	sel := mysql.SelectMaster(cluster, probes)
	if !sel.SplitBrain || sel.Master != nil {
		t.Fatalf("expected split brain, got %#v", sel)
	}
}

func TestEnsureDatabaseNeverDrops(t *testing.T) {
	created := false
	script := &mysqltest.Script{
		ExecFunc: func(query string) error {
			if strings.Contains(query, "CREATE DATABASE") {
				created = true
			}
			return nil
		},
		QueryFunc: func(query string) ([][]any, error) {
			if strings.Contains(query, "SCHEMATA") && created {
				return [][]any{{"utf8mb3", "utf8mb3_general_ci"}}, nil
			}
			return nil, nil
		},
	}
	state, err := mysql.EnsureDatabase(context.Background(), script, "payments_main", "utf8mb3", "utf8mb3_general_ci")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exists || !script.Contains("CREATE DATABASE") {
		t.Fatalf("state=%#v queries=%v", state, script.Queries)
	}
	if script.Contains("DROP DATABASE") || script.Contains("DROP SCHEMA") {
		t.Fatalf("database was dropped: %v", script.Queries)
	}
	again, err := mysql.EnsureDatabase(context.Background(), script, "payments_main", "utf8mb4", "utf8mb4_general_ci")
	if err != nil {
		t.Fatal(err)
	}
	if again.Charset != "utf8mb3" {
		t.Fatalf("existing charset changed: %#v", again)
	}
}

func TestApplyAccountDoesNotDropDatabase(t *testing.T) {
	script := &mysqltest.Script{QueryFunc: func(query string) ([][]any, error) {
		switch {
		case strings.Contains(query, "mysql.user"):
			return nil, nil
		case strings.Contains(query, "SCHEMATA"):
			return [][]any{{"utf8mb3", "utf8mb3_general_ci"}}, nil
		case strings.HasPrefix(query, "SHOW GRANTS"):
			return [][]any{{"GRANT USAGE ON *.* TO `app`@`%`"}}, nil
		default:
			return nil, nil
		}
	}}
	err := mysql.ApplyAccount(context.Background(), script, mysql.Account{
		User: "app", Host: "%", Plugin: mysqlv1alpha1.AuthPluginCachingSHA2,
		Password: "secret", ApplyPassword: true, RequireTLS: true,
		Grants: []mysqlv1alpha1.GrantSpec{{Database: "payments_main", Privileges: []string{"SELECT"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !script.Contains("CREATE USER") || !script.Contains("REQUIRE SSL") || !script.Contains("GRANT SELECT") {
		t.Fatalf("queries = %v", script.Queries)
	}
	if script.Contains("DROP DATABASE") {
		t.Fatal(script.Queries)
	}
	if err := mysql.DropAccount(context.Background(), script, "app", "%"); err != nil {
		t.Fatal(err)
	}
	if !script.Contains("DROP USER") || script.Contains("DROP DATABASE") {
		t.Fatal(script.Queries)
	}
}

func TestRejectDropDatabase(t *testing.T) {
	script := &mysqltest.Script{}
	if err := script.Exec(context.Background(), "DROP DATABASE payments"); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestParseGrantsAndDiff(t *testing.T) {
	actual, err := mysql.ParseGrants([]string{
		"GRANT USAGE ON *.* TO `app`@`%`",
		"GRANT SELECT, UPDATE ON `payments_main`.* TO `app`@`%`",
	})
	if err != nil {
		t.Fatal(err)
	}
	statements, err := mysql.GrantStatements("app", "%", []mysql.DesiredGrant{{
		Database: "payments_main", Table: "*", Privileges: []string{"SELECT", "INSERT"},
	}}, actual)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(statements, "\n")
	if !strings.Contains(joined, "GRANT INSERT") || !strings.Contains(joined, "REVOKE UPDATE") {
		t.Fatalf("statements = %s", joined)
	}
}

func TestSupportsDualPassword(t *testing.T) {
	if !mysql.SupportsDualPassword("8.0.36") || !mysql.SupportsDualPassword("8.4.0") {
		t.Fatal("expected dual password support")
	}
	if mysql.SupportsDualPassword("8.0.13") || mysql.SupportsDualPassword("5.7.42-log") {
		t.Fatal("old servers should not retain passwords")
	}
}
