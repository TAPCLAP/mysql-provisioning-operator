package mysql

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

func TestTLSConfigDisabled(t *testing.T) {
	cfg, err := (Session{}).tlsConfig(mysqlv1alpha1.MySQLMember{Host: "db1"})
	if err != nil || cfg != nil {
		t.Fatalf("cfg=%v err=%v", cfg, err)
	}
}

func TestTLSConfigRequired(t *testing.T) {
	certPEM, _ := testCertificate(t)
	cfg, err := (Session{
		TLS: &mysqlv1alpha1.ClusterTLS{Mode: mysqlv1alpha1.TLSModeRequired, ServerName: "db.internal"},
		CA:  certPEM,
	}).tlsConfig(mysqlv1alpha1.MySQLMember{Host: "db1-1"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "db.internal" || cfg.InsecureSkipVerify || cfg.RootCAs == nil || cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("%#v", cfg)
	}
	if _, err := (Session{
		TLS: &mysqlv1alpha1.ClusterTLS{Mode: mysqlv1alpha1.TLSModeRequired},
		CA:  []byte("not a certificate"),
	}).tlsConfig(mysqlv1alpha1.MySQLMember{Host: "db1-1"}); err == nil {
		t.Fatal("invalid CA was accepted")
	}
}

func TestTLSConfigSkipVerifyUsesMemberHost(t *testing.T) {
	cfg, err := (Session{
		TLS: &mysqlv1alpha1.ClusterTLS{Mode: mysqlv1alpha1.TLSModeSkipVerify},
	}).tlsConfig(mysqlv1alpha1.MySQLMember{Host: "db1-1.internal"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.InsecureSkipVerify || cfg.ServerName != "db1-1.internal" {
		t.Fatalf("%#v", cfg)
	}
}

func TestTLSConfigClientCertificate(t *testing.T) {
	certPEM, keyPEM := testCertificate(t)
	cfg, err := (Session{
		TLS:        &mysqlv1alpha1.ClusterTLS{Mode: mysqlv1alpha1.TLSModeRequired},
		ClientCert: certPEM,
		ClientKey:  keyPEM,
	}).tlsConfig(mysqlv1alpha1.MySQLMember{Host: "db1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("certificates = %d", len(cfg.Certificates))
	}
}

func testCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"db.internal"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}
