// Package pbtls implements password-based TLS.
package pbtls

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// ConnectionKey is a seed for an ed25519 private key with which the fundamental CA certificate is signed.
type ConnectionKey [ed25519.SeedSize]byte

// ParseConnectionKey parses a base64-encoded connection key.
func ParseConnectionKey(key string) (ConnectionKey, error) {
	var connectionKey ConnectionKey

	if key == "" {
		return connectionKey, fmt.Errorf("connection key is empty")
	}

	keyBytes, err := base64.RawStdEncoding.DecodeString(key)
	if err != nil {
		return connectionKey, fmt.Errorf("base64 decode: %w", err)
	}

	err = checkKeyBytes(keyBytes)
	if err != nil {
		return connectionKey, err
	}

	n := copy(connectionKey[:], keyBytes)
	if n != ed25519.SeedSize { // just in case
		return connectionKey, fmt.Errorf("only %d bytes were copied instead of %d", n, ed25519.SeedSize)
	}

	return connectionKey, nil
}

// GenerateConnectionKey generates a new connection key.
func GenerateConnectionKey() (ConnectionKey, error) {
	var connectionKey ConnectionKey

	maxAttempts := 10

	for i := 0; i < maxAttempts; i++ {
		n, err := rand.Read(connectionKey[:])
		if err != nil {
			return connectionKey, fmt.Errorf("read random bytes: %w", err)
		}

		if n != ed25519.SeedSize { // just in case
			return connectionKey, fmt.Errorf("only %d bytes were generated instead of %d", n, ed25519.SeedSize)
		}

		if isZero(connectionKey[:]) {
			continue
		}

		return connectionKey, nil
	}

	err := checkKeyBytes(connectionKey[:])
	if err != nil {
		return connectionKey, err
	}

	return connectionKey, fmt.Errorf("could not generate a valid non-zero connection key in %d attempts", maxAttempts)
}

// String returns the connection key as a base64-encoded string.
func (key ConnectionKey) String() string {
	return base64.RawStdEncoding.EncodeToString(key[:])
}

// PublicKey returns the base64-encoded ed25519 public key that corresponds to the connection key.
func (key ConnectionKey) PublicKey() string {
	//nolint:forcetypeassert
	return base64.RawStdEncoding.EncodeToString(ed25519.NewKeyFromSeed(key[:]).Public().(ed25519.PublicKey))
}

// generateCA generates a deterministic CA certificate that never expires.
func generateCA(key ConnectionKey) (caCert *x509.Certificate, caKey crypto.PrivateKey, err error) {
	err = checkKeyBytes(key[:])
	if err != nil {
		return nil, nil, err
	}

	privateKey := ed25519.NewKeyFromSeed(key[:])

	caCert = &x509.Certificate{
		SerialNumber: big.NewInt(1337),
		Subject: pkix.Name{
			CommonName: key.PublicKey(),
		},
		NotBefore:             time.Unix(0, 0),
		NotAfter:              time.Date(9999, 1, 1, 0, 0, 0, 0, time.FixedZone("", 0)),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	certBytes, err := x509.CreateCertificate(nil, caCert, caCert, privateKey.Public(), privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("generate certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse certificate: %w", err)
	}

	return cert, privateKey, nil
}

// generateCertificate generates an x509 certificate.
func generateCertificate(
	caCert *x509.Certificate, caKey crypto.PrivateKey, hostname string, usage x509.ExtKeyUsage,
) (pemCert []byte, pemKey []byte, err error) {
	now := time.Now()

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate certificate key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		DNSNames:              []string{hostname},
		NotBefore:             now,
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{usage},
		BasicConstraintsValid: true,
	}

	derCert, err := x509.CreateCertificate(rand.Reader, template, caCert, pubKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create client certificate: %w", err)
	}

	pkcs8Key, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal private key: %w", err)
	}

	pemCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derCert})
	pemKey = pem.EncodeToMemory(&pem.Block{Type: "ED25519 PRIVATE KEY", Bytes: pkcs8Key})

	return pemCert, pemKey, nil
}

// ServerTLSConfig generates a TLS server config based on the connection key.
// The server certificate will use the connection keys public key as server
// DNS name.
func ServerTLSConfig(key ConnectionKey) (*tls.Config, error) {
	return ServerTLSConfigForHostname(key, key.PublicKey())
}

// ServerTLSConfigForHostname generates a TLS server config based on the
// connection key with the provided hostname in the server certificate's DNS
// name section.
func ServerTLSConfigForHostname(key ConnectionKey, hostname string) (*tls.Config, error) {
	ca, caKey, err := generateCA(key)
	if err != nil {
		return nil, fmt.Errorf("generate CA: %w", err)
	}

	clientCAPool := x509.NewCertPool()
	clientCAPool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}))

	pemServerCert, pemServerKey, err := generateCertificate(ca, caKey, hostname, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return nil, fmt.Errorf("generate server certificate: %w", err)
	}

	cert, err := tls.X509KeyPair(pemServerCert, pemServerKey)
	if err != nil {
		return nil, fmt.Errorf("load server certificate: %w", err)
	}

	cfg := &tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAPool,
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	return cfg, nil
}

// ClientTLSConfig generates a TLS client config based on the connection key.
// The client certificate's DNS name will be the connection keys's public key
// which is also set as ServerName in the returned *tls.Config.
func ClientTLSConfig(key ConnectionKey) (*tls.Config, error) {
	return ClientTLSConfigForClientName(key, key.PublicKey())
}

// ClientTLSConfigForClientName generates a TLS client config for an arbitrary
// client DNS name. Note that the ServerName attribute is still set to the
// connection key's public key.
func ClientTLSConfigForClientName(key ConnectionKey, clientName string) (*tls.Config, error) {
	ca, caKey, err := generateCA(key)
	if err != nil {
		return nil, fmt.Errorf("generate CA: %w", err)
	}

	pemClientCert, pemClientKey, err := generateCertificate(ca, caKey, key.PublicKey(), x509.ExtKeyUsageClientAuth)
	if err != nil {
		return nil, fmt.Errorf("generate client certificate: %w", err)
	}

	clientCert, err := tls.X509KeyPair(pemClientCert, pemClientKey)
	if err != nil {
		return nil, fmt.Errorf("load client certificate: %w", err)
	}

	rootCAPool := x509.NewCertPool()
	rootCAPool.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}))

	cfg := &tls.Config{
		RootCAs:      rootCAPool,
		Certificates: []tls.Certificate{clientCert},
		ServerName:   key.PublicKey(),
		MinVersion:   tls.VersionTLS13,
	}

	return cfg, nil
}

func checkKeyBytes(key []byte) error {
	if len(key) != ed25519.SeedSize {
		return fmt.Errorf("key has only %d bytes instead of %d", len(key), ed25519.SeedSize)
	}

	if isZero(key) {
		return fmt.Errorf("invalid all-zero connection key")
	}

	return nil
}

func isZero(s []byte) bool {
	for _, v := range s {
		if v != 0 {
			return false
		}
	}

	return true
}
