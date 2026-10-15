package mysql

import (
	"context"
	"fmt"
	"strconv"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

// Probe is the result of inspecting one member.
type Probe struct {
	Reachable             bool
	ReadOnly              bool
	SuperReadOnly         bool
	ServerID              string
	ReplicationConfigured bool
	ReplicationUnknown    bool
	Message               string
}

// Selection is the master choice for one probe pass.
type Selection struct {
	Master            *mysqlv1alpha1.MySQLMember
	Reason            string
	NoMaster          bool
	SplitBrain        bool
	MasterUnavailable bool
	InvalidCriteria   bool
	WouldPass         *bool
	Probes            map[string]Probe
	CriteriaMatched   map[string]bool
}

// ProbeMember reads read_only, super_read_only, server_id, and replication configuration.
func ProbeMember(ctx context.Context, db DB) (Probe, error) {
	rows, err := db.Query(ctx, "SELECT @@global.read_only, @@global.super_read_only, CAST(@@global.server_id AS CHAR)")
	if err != nil {
		return Probe{Message: err.Error()}, err
	}
	defer func() { _ = rows.Close() }()
	var probe Probe
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Probe{Message: err.Error()}, err
		}
		return Probe{Message: "empty read_only probe"}, fmt.Errorf("empty read_only probe")
	}
	var readOnly, superReadOnly int64
	if err := rows.Scan(&readOnly, &superReadOnly, &probe.ServerID); err != nil {
		return Probe{Message: err.Error()}, err
	}
	probe.Reachable = true
	probe.ReadOnly = readOnly != 0
	probe.SuperReadOnly = superReadOnly != 0
	configured, unknown, message := replicationConfigured(ctx, db)
	probe.ReplicationConfigured = configured
	probe.ReplicationUnknown = unknown
	probe.Message = message
	return probe, nil
}

func replicationConfigured(ctx context.Context, db DB) (configured bool, unknown bool, message string) {
	rows, err := db.Query(ctx, "SELECT COUNT(*) FROM performance_schema.replication_connection_configuration")
	if err == nil {
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			var count int64
			if scanErr := rows.Scan(&count); scanErr != nil {
				return false, true, scanErr.Error()
			}
			return count > 0, false, ""
		}
		if err := rows.Err(); err != nil {
			return false, true, err.Error()
		}
	}
	for _, query := range []string{"SHOW REPLICA STATUS", "SHOW SLAVE STATUS"} {
		status, qErr := db.Query(ctx, query)
		if qErr != nil {
			message = qErr.Error()
			continue
		}
		hasRow := status.Next()
		_ = status.Close()
		return hasRow, false, ""
	}
	return false, true, message
}

// ProbeAll dials every member. A dial error is stored on that member and does not abort the others.
func ProbeAll(ctx context.Context, opener Opener, cluster *mysqlv1alpha1.MySQLCluster, user, password string) map[string]Probe {
	out := make(map[string]Probe, len(cluster.Spec.Members))
	for _, member := range cluster.Spec.Members {
		db, err := opener.Open(ctx, member, user, password)
		if err != nil {
			out[member.Name] = Probe{Message: err.Error()}
			continue
		}
		probe, err := ProbeMember(ctx, db)
		_ = db.Close()
		if err != nil && probe.Message == "" {
			probe.Message = err.Error()
		}
		if err != nil {
			probe.Reachable = false
		}
		out[member.Name] = probe
	}
	return out
}

// SelectMaster chooses the member that should receive Master-target DDL.
func SelectMaster(cluster *mysqlv1alpha1.MySQLCluster, probes map[string]Probe) Selection {
	sel := Selection{
		Probes:          probes,
		CriteriaMatched: map[string]bool{},
	}
	if cluster.Spec.Master.Criteria != nil && !cluster.Spec.Master.Criteria.AnyEnabled() {
		sel.InvalidCriteria = true
		sel.NoMaster = true
		return sel
	}
	readOnlyOff, superOff, noRepl := cluster.Spec.Master.Criteria.EffectiveCriteria()
	var matched []mysqlv1alpha1.MySQLMember
	for _, member := range cluster.Spec.Members {
		probe := probes[member.Name]
		ok := matches(probe, readOnlyOff, superOff, noRepl)
		sel.CriteriaMatched[member.Name] = ok
		if ok {
			matched = append(matched, member)
		}
	}
	if cluster.Spec.Master.Mode == mysqlv1alpha1.MasterModeExplicit {
		return selectExplicit(cluster, sel, matched)
	}
	switch len(matched) {
	case 0:
		sel.NoMaster = true
	case 1:
		member := matched[0]
		sel.Master = &member
		sel.Reason = string(mysqlv1alpha1.MasterModeDetected)
	default:
		sel.SplitBrain = true
	}
	return sel
}

func selectExplicit(cluster *mysqlv1alpha1.MySQLCluster, sel Selection, matched []mysqlv1alpha1.MySQLMember) Selection {
	var explicit []mysqlv1alpha1.MySQLMember
	for _, member := range cluster.Spec.Members {
		if member.Master {
			explicit = append(explicit, member)
		}
	}
	if len(explicit) != 1 {
		sel.NoMaster = true
		return sel
	}
	member := explicit[0]
	probe := sel.Probes[member.Name]
	would := containsMember(matched, member.Name)
	sel.WouldPass = &would
	if !probe.Reachable {
		sel.MasterUnavailable = true
		return sel
	}
	sel.Master = &member
	sel.Reason = string(mysqlv1alpha1.MasterModeExplicit)
	return sel
}

func matches(probe Probe, readOnlyOff, superOff, noRepl bool) bool {
	if !probe.Reachable {
		return false
	}
	if readOnlyOff && probe.ReadOnly {
		return false
	}
	if superOff && probe.SuperReadOnly {
		return false
	}
	if noRepl && (probe.ReplicationConfigured || probe.ReplicationUnknown) {
		return false
	}
	return true
}

func containsMember(members []mysqlv1alpha1.MySQLMember, name string) bool {
	for _, member := range members {
		if member.Name == name {
			return true
		}
	}
	return false
}

// MemberStatus converts probes into the API status form.
func MemberStatus(cluster *mysqlv1alpha1.MySQLCluster, sel Selection) []mysqlv1alpha1.MemberStatus {
	out := make([]mysqlv1alpha1.MemberStatus, 0, len(cluster.Spec.Members))
	for _, member := range cluster.Spec.Members {
		probe := sel.Probes[member.Name]
		status := mysqlv1alpha1.MemberStatus{
			Name:            member.Name,
			Reachable:       probe.Reachable,
			ServerID:        probe.ServerID,
			MatchesCriteria: sel.CriteriaMatched[member.Name],
			Message:         probe.Message,
		}
		if probe.Reachable {
			readOnly := probe.ReadOnly
			superReadOnly := probe.SuperReadOnly
			repl := probe.ReplicationConfigured
			status.ReadOnly = &readOnly
			status.SuperReadOnly = &superReadOnly
			status.ReplicationConfigured = &repl
		}
		out = append(out, status)
	}
	return out
}

// FormatBool is a small helper for tests and status messages.
func FormatBool(v bool) string {
	return strconv.FormatBool(v)
}
