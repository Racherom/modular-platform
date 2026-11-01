// Package testpki is a certificate authority for the sdk's own tests: it signs ECDSA P-256 leaf
// certificates for any names, valid for any lifetime from now.
package testpki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

// CA signs test certificates.
type CA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// New returns a new CA, valid for ten years from now.
func New(t testing.TB) *CA {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: "sdk test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &CA{cert: cert, key: key}
}

// PEM is the CA's certificate in PEM, the content of an MP_CA_FILE.
func (ca *CA) PEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
}

// Pool holds the CA's certificate.
func (ca *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.cert)
	return p
}

// Leaf returns a server certificate for names (DNS names or IP addresses), valid from now for
// lifetime, with its Leaf set.
func (ca *CA) Leaf(t testing.TB, lifetime time.Duration, names ...string) *tls.Certificate {
	t.Helper()
	now := time.Now()
	return ca.LeafBetween(t, now, now.Add(lifetime), names...)
}

// LeafBetween returns a server certificate for names, valid from notBefore to notAfter, with its
// Leaf set.
func (ca *CA) LeafBetween(t testing.TB, notBefore, notAfter time.Time, names ...string) *tls.Certificate {
	t.Helper()
	key := newKey(t)
	leaf := ca.Sign(t, &key.PublicKey, notBefore, notAfter, names...)
	return &tls.Certificate{Certificate: [][]byte{leaf.Raw}, PrivateKey: key, Leaf: leaf}
}

// Sign returns a server certificate for pub and names, valid from notBefore to notAfter.
func (ca *CA) Sign(t testing.TB, pub crypto.PublicKey, notBefore, notAfter time.Time, names ...string) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: serial(t),
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, pub, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func newKey(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func serial(t testing.TB) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
