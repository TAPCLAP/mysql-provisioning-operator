//go:build integration

package mysqltest

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
)

// Server is one MySQL container with a certificate the operator can verify.
type Server struct {
	Member    mysqlv1alpha1.MySQLMember
	Admin     mysql.Session
	CA        []byte
	container testcontainers.Container
}

// Start runs mysql:8.4 with TLS and waits until it accepts an admin connection.
func Start(t *testing.T, ctx context.Context) *Server {
	t.Helper()
	dir := t.TempDir()
	caPEM, serverCert, serverKey := serverCertificate(t)
	writeFile(t, filepath.Join(dir, "ca.pem"), caPEM)
	writeFile(t, filepath.Join(dir, "server-cert.pem"), serverCert)
	writeFile(t, filepath.Join(dir, "server-key.pem"), serverKey)
	writeFile(t, filepath.Join(dir, "ssl.cnf"), []byte(`
[mysqld]
ssl_ca=/var/lib/mysql-ssl/ca.pem
ssl_cert=/var/lib/mysql-ssl/server-cert.pem
ssl_key=/var/lib/mysql-ssl/server-key.pem
`))

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "mysql:8.4",
			ExposedPorts: []string{"3306/tcp"},
			Env: map[string]string{
				"MYSQL_ROOT_PASSWORD": "secret",
				"MYSQL_ROOT_HOST":     "%",
			},
			Entrypoint: []string{"bash", "-c"},
			Cmd: []string{`
set -eu
mkdir -p /var/lib/mysql-ssl
cp /certs/ca.pem /certs/server-cert.pem /certs/server-key.pem /var/lib/mysql-ssl/
chown mysql:mysql /var/lib/mysql-ssl /var/lib/mysql-ssl/ca.pem /var/lib/mysql-ssl/server-cert.pem /var/lib/mysql-ssl/server-key.pem
chmod 755 /var/lib/mysql-ssl
chmod 644 /var/lib/mysql-ssl/ca.pem /var/lib/mysql-ssl/server-cert.pem
chmod 600 /var/lib/mysql-ssl/server-key.pem
exec /usr/local/bin/docker-entrypoint.sh mysqld
`},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: filepath.Join(dir, "ca.pem"), ContainerFilePath: "/certs/ca.pem", FileMode: 0o644},
				{HostFilePath: filepath.Join(dir, "server-cert.pem"), ContainerFilePath: "/certs/server-cert.pem", FileMode: 0o644},
				{HostFilePath: filepath.Join(dir, "server-key.pem"), ContainerFilePath: "/certs/server-key.pem", FileMode: 0o600},
				{HostFilePath: filepath.Join(dir, "ssl.cnf"), ContainerFilePath: "/etc/mysql/conf.d/ssl.cnf", FileMode: 0o644},
			},
			WaitingFor: wait.ForLog("port: 3306").WithStartupTimeout(3 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start mysql: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := container.MappedPort(ctx, "3306/tcp")
	if err != nil {
		t.Fatal(err)
	}
	port := mapped.Int()
	if port <= 0 || port > 65535 {
		t.Fatalf("mapped port %d", port)
	}
	server := &Server{
		Member: mysqlv1alpha1.MySQLMember{Name: "db1-1", Host: host, Port: int32(port)},
		Admin: mysql.Session{
			User:     "root",
			Password: "secret",
			TLS:      &mysqlv1alpha1.ClusterTLS{Mode: mysqlv1alpha1.TLSModeRequired, ServerName: "localhost"},
			CA:       caPEM,
		},
		CA:        caPEM,
		container: container,
	}
	db := server.Open(t, ctx, server.Admin)
	_ = db.Close()
	return server
}

// Open dials the server, retrying while it is still starting.
func (s *Server) Open(t *testing.T, ctx context.Context, session mysql.Session) mysql.DB {
	t.Helper()
	db, err := s.open(ctx, session, 20)
	if err != nil {
		s.DumpLogs(t, ctx)
		t.Fatalf("connect: %v", err)
	}
	return db
}

// TryOpen dials once. The caller decides whether an error is expected.
func (s *Server) TryOpen(ctx context.Context, session mysql.Session) (mysql.DB, error) {
	return (mysql.SQLOpener{}).Open(ctx, s.Member, session)
}

func (s *Server) open(ctx context.Context, session mysql.Session, attempts int) (mysql.DB, error) {
	var last error
	for range attempts {
		db, err := s.TryOpen(ctx, session)
		if err == nil {
			return db, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, last
}

// DumpLogs writes the MySQL container log to the test output.
func (s *Server) DumpLogs(t *testing.T, ctx context.Context) {
	t.Helper()
	reader, err := s.container.Logs(ctx)
	if err != nil {
		t.Logf("container logs: %v", err)
		return
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Logf("container logs: %v", err)
		return
	}
	t.Logf("container logs:\n%s", body)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func serverCertificate(t *testing.T) (caPEM, certPEM, keyPEM []byte) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "mysql-test-ca"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})
}
