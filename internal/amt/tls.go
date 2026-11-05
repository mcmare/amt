package amt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func trustConfig(caFile, serverName string) (*tls.Config, error) {
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		if ok := roots.AppendCertsFromPEM(pem); !ok {
			return nil, fmt.Errorf("no PEM certificates in %s", caFile)
		}
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: serverName}, nil
}

func serverTLS(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, NextProtos: []string{"h2", "http/1.1"}}, nil
}
