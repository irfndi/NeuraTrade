package grpcutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
)

func TestDialOptions_EmptyCAIsInsecure(t *testing.T) {
	opts, err := DialOptions(config.GRPCClientConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) == 0 {
		t.Fatal("expected at least one dial option")
	}
}

func TestDialOptions_ValidCAFileReturnsOptions(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, generateTestCA(t), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	opts, err := DialOptions(config.GRPCClientConfig{
		TLSCACertFile: caFile,
		ServerName:    "localhost",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) == 0 {
		t.Fatal("expected at least one dial option")
	}
}

func TestDialOptions_MissingCAFileFails(t *testing.T) {
	_, err := DialOptions(config.GRPCClientConfig{
		TLSCACertFile: "/no/such/file.pem",
	})
	if err == nil {
		t.Fatal("expected error for missing CA file")
	}
}

func TestDialOptions_GarbageCAFileFails(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, []byte("not a cert"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := DialOptions(config.GRPCClientConfig{TLSCACertFile: caFile})
	if err == nil {
		t.Fatal("expected error for non-PEM CA file")
	}
}

func TestDialOptions_UnsupportedAuthTypeFails(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, generateTestCA(t), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	_, err := DialOptions(config.GRPCClientConfig{
		TLSCACertFile: caFile,
		TLSAuthType:   "unknown",
	})
	if err == nil {
		t.Fatal("expected error for unsupported auth type")
	}
}

func TestDialOptions_MTLSAuthTypeExplicitlyUnwired(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, generateTestCA(t), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	_, err := DialOptions(config.GRPCClientConfig{
		TLSCACertFile: caFile,
		TLSAuthType:   "mtls",
	})
	if err == nil {
		t.Fatal("expected error for mTLS auth type (not yet wired)")
	}
}

func generateTestCA(t *testing.T) []byte {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
}
