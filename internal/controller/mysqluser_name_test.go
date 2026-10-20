package controller

import (
	"strings"
	"testing"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
)

const (
	member1    = "db1-1"
	member2    = "db1-2"
	member3    = "db1-3"
	publicHost = "db.example.com"
)

func TestRandomSuffix(t *testing.T) {
	got, err := randomSuffix(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("suffix length %d", len(got))
	}
	for _, r := range got {
		if !strings.ContainsRune(usernameAlphabet, r) {
			t.Fatalf("suffix character %q", r)
		}
	}
}

func TestAccountNameUsesRecordedName(t *testing.T) {
	user := &mysqlv1alpha1.MySQLUser{}
	user.Spec.Username = "user"
	if accountName(user) != "user" {
		t.Fatalf("account name %q", accountName(user))
	}
	user.Status.Username = "usera1b2c3d4"
	if accountName(user) != "usera1b2c3d4" {
		t.Fatalf("recorded account name %q", accountName(user))
	}
}

func TestReplicaEndpoint(t *testing.T) {
	cluster := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{Members: []mysqlv1alpha1.MySQLMember{
		{Name: member1, Host: member1},
		{Name: member2, Host: member2},
		{Name: member3, Host: member3},
	}}}
	target := resolved{Cluster: cluster, Selection: mysql.Selection{Master: &cluster.Spec.Members[1]}}
	got := replicaEndpoint(target)
	if got.Name != member1 || got.Host != member1 || got.Index != 1 || got.Port != 3306 {
		t.Fatalf("first non-master %#v", got)
	}
	target.Selection.Master = &cluster.Spec.Members[0]
	got = replicaEndpoint(target)
	if got.Name != member2 || got.Index != 2 {
		t.Fatalf("replica after master %#v", got)
	}
	target.Selection.Master = nil
	if replicaEndpoint(target).Name != "" {
		t.Fatal("replica was chosen without a master")
	}
	alone := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{Members: []mysqlv1alpha1.MySQLMember{
		{Name: member1, Host: member1},
	}}}
	only := resolved{Cluster: alone, Selection: mysql.Selection{Master: &alone.Spec.Members[0]}}
	if replicaEndpoint(only).Name != "" {
		t.Fatal("the only member was used as a replica")
	}
}

func TestAttributesReachTemplateAndStatus(t *testing.T) {
	cluster := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{Members: []mysqlv1alpha1.MySQLMember{
		{Name: member1, Host: member1, Attributes: map[string]string{"publicHost": publicHost}},
		{Name: member2, Host: member2, Attributes: map[string]string{"zone": "a"}},
	}}}
	target := resolved{Cluster: cluster, Selection: mysql.Selection{Master: &cluster.Spec.Members[0]}, LogBin: true}
	members := memberEndpoints(target)
	if members[0].Attributes["publicHost"] != publicHost || members[1].Attributes["zone"] != "a" {
		t.Fatalf("template members %#v", members)
	}
	members[0].Attributes["publicHost"] = "changed"
	if cluster.Spec.Members[0].Attributes["publicHost"] != publicHost {
		t.Fatal("template attributes share the spec map")
	}
	master := masterEndpoint(target)
	if master.Index != 1 || master.Attributes["publicHost"] != publicHost {
		t.Fatalf("template master %#v", master)
	}
	replica := replicaEndpoint(target)
	if replica.Name != member2 || replica.Index != 2 || replica.Attributes["zone"] != "a" {
		t.Fatalf("template replica %#v", replica)
	}
	statusMembers, statusMaster := recordPlacement(target, []string{member1})
	if len(statusMembers) != 2 || statusMembers[0].Attributes["publicHost"] != publicHost || statusMembers[1].Attributes["zone"] != "a" {
		t.Fatalf("status members %#v", statusMembers)
	}
	if statusMaster == nil || statusMaster.Attributes["publicHost"] != publicHost {
		t.Fatalf("status master %#v", statusMaster)
	}
	statusMaster.Attributes["publicHost"] = "changed"
	if cluster.Spec.Members[0].Attributes["publicHost"] != publicHost {
		t.Fatal("status attributes share the spec map")
	}
}

func TestRecordPlacementListsEveryMemberOnMasterTarget(t *testing.T) {
	cluster := &mysqlv1alpha1.MySQLCluster{Spec: mysqlv1alpha1.MySQLClusterSpec{Members: []mysqlv1alpha1.MySQLMember{
		{Name: member1, Host: member1},
		{Name: member2, Host: member2},
	}}}
	target := resolved{
		Cluster:   cluster,
		LogBin:    true,
		Selection: mysql.Selection{Master: &cluster.Spec.Members[0]},
	}
	members, master := recordPlacement(target, []string{member1})
	if master == nil || master.Name != member1 || len(members) != 2 || members[0].Name != member1 || members[1].Name != member2 || members[1].Port != 3306 || memberNameList(members) != member1+","+member2 {
		t.Fatalf("master target members=%#v master=%#v", members, master)
	}
	target.LogBin = false
	members, _ = recordPlacement(target, []string{member1})
	if len(members) != 1 || members[0].Name != member1 {
		t.Fatalf("all target %#v", members)
	}
}
