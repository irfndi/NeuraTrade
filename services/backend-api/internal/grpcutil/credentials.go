package grpcutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/irfndi/neuratrade/internal/config"
)

func DialOptions(cfg config.GRPCClientConfig) ([]grpc.DialOption, error) {
	if cfg.TLSCACertFile == "" {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, nil
	}

	caPEM, err := os.ReadFile(cfg.TLSCACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA bundle %q: %w", cfg.TLSCACertFile, err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("CA bundle %q contains no valid PEM certificates", cfg.TLSCACertFile)
	}

	tlsCfg := &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
		ServerName: cfg.ServerName,
	}

	switch cfg.TLSAuthType {
	case "", "tls":
		return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg))}, nil
	case "mtls":
		return nil, fmt.Errorf("mTLS client cert loading is not yet wired (TLSAuthType=%q); set TLSAuthType=tls for server-cert validation only", cfg.TLSAuthType)
	default:
		return nil, fmt.Errorf("unsupported grpc tls_auth_type %q (want \"tls\" or \"mtls\")", cfg.TLSAuthType)
	}
}

