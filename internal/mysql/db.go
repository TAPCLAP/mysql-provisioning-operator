package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

// Rows is the subset of *sql.Rows the operator uses.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

// DB is one session to a MySQL server.
type DB interface {
	Exec(ctx context.Context, query string) error
	Query(ctx context.Context, query string) (Rows, error)
	Close() error
}

// Session is the admin login and TLS material for one cluster.
type Session struct {
	User       string
	Password   string
	TLS        *mysqlv1alpha1.ClusterTLS
	CA         []byte
	ClientCert []byte
	ClientKey  []byte
}

// Opener dials a cluster member with the admin account.
type Opener interface {
	Open(ctx context.Context, member mysqlv1alpha1.MySQLMember, session Session) (DB, error)
}

// SQLOpener opens real MySQL connections.
type SQLOpener struct{}

// Open dials member and pings it.
func (SQLOpener) Open(ctx context.Context, member mysqlv1alpha1.MySQLMember, session Session) (DB, error) {
	cfg := mysqldriver.NewConfig()
	cfg.User = session.User
	cfg.Passwd = session.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(member.Host, strconv.Itoa(int(member.AddressPort())))
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 15 * time.Second
	cfg.WriteTimeout = 15 * time.Second
	cfg.ParseTime = true
	cfg.AllowNativePasswords = true
	cfg.MultiStatements = false
	tlsConfig, err := session.tlsConfig(member)
	if err != nil {
		return nil, err
	}
	cfg.TLS = tlsConfig
	connector, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqlDB{DB: db}, nil
}

type sqlDB struct {
	*sql.DB
}

func (s *sqlDB) Exec(ctx context.Context, query string) error {
	_, err := s.ExecContext(ctx, query)
	return err
}

func exec(ctx context.Context, db DB, query string) error {
	if err := rejectDropDatabase(query); err != nil {
		return err
	}
	return db.Exec(ctx, query)
}

func (s *sqlDB) Query(ctx context.Context, query string) (Rows, error) {
	if err := rejectDropDatabase(query); err != nil {
		return nil, err
	}
	return s.QueryContext(ctx, query)
}

func rejectDropDatabase(query string) error {
	upper := strings.ToUpper(query)
	if strings.Contains(upper, "DROP DATABASE") || strings.Contains(upper, "DROP SCHEMA") {
		return fmt.Errorf("refusing to drop a database")
	}
	return nil
}

func (s Session) tlsConfig(member mysqlv1alpha1.MySQLMember) (*tls.Config, error) {
	if s.TLS == nil || !s.TLS.Enabled() {
		return nil, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	name := s.TLS.ServerName
	if name == "" {
		name = member.Host
	}
	cfg.ServerName = name
	switch s.TLS.Mode {
	case mysqlv1alpha1.TLSModeSkipVerify:
		cfg.InsecureSkipVerify = true
	case mysqlv1alpha1.TLSModeRequired:
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if len(s.CA) > 0 && !pool.AppendCertsFromPEM(s.CA) {
			return nil, fmt.Errorf("tls CA is not a PEM certificate bundle")
		}
		cfg.RootCAs = pool
	default:
		return nil, fmt.Errorf("unsupported tls mode %q", s.TLS.Mode)
	}
	if len(s.ClientCert) == 0 && len(s.ClientKey) == 0 {
		return cfg, nil
	}
	cert, err := tls.X509KeyPair(s.ClientCert, s.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("tls client certificate: %w", err)
	}
	cfg.Certificates = []tls.Certificate{cert}
	return cfg, nil
}

// PrepareSession disables the binary log when logBin is false.
func PrepareSession(ctx context.Context, db DB, logBin bool) error {
	if logBin {
		return nil
	}
	return db.Exec(ctx, "SET SESSION sql_log_bin = 0")
}
