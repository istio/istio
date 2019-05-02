package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"time"
)

// KeyPair stores an x509 certificate and its ECDSA private key
type KeyPair struct {
	Cert *x509.Certificate
	Priv *ecdsa.PrivateKey
}

// ToPEM returns the PEM encoded cert pair
func (kp *KeyPair) ToPEM() (certPEM []byte, privPEM []byte, err error) {
	// PEM encode private key
	privDER, err := x509.MarshalECPrivateKey(kp.Priv)
	if err != nil {
		return
	}
	privBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privDER,
	}
	privPEM = pem.EncodeToMemory(privBlock)

	// PEM encode cert
	certBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: kp.Cert.Raw,
	}
	certPEM = pem.EncodeToMemory(certBlock)

	return
}

// GenerateCA generates a self-signed CA cert/key pair that expires in expiresIn days
func GenerateCA(notAfter time.Time, organization string) (*KeyPair, error) {
	notBefore := time.Now()
	if notAfter.Before(notBefore) {
		return nil, fmt.Errorf("invalid notAfter: %s before %s", notAfter.String(), notBefore.String())
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).SetInt64(math.MaxInt64))
	if err != nil {
		return nil, err
	}

	caDetails := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{organization},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	publicKey := &privateKey.PublicKey
	certRaw, err := x509.CreateCertificate(rand.Reader, caDetails, caDetails, publicKey, privateKey)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(certRaw)
	if err != nil {
		return nil, err
	}

	ca := &KeyPair{
		Cert: cert,
		Priv: privateKey,
	}

	return ca, nil
}

// CreateSignedServingPair creates a serving cert/key pair signed by the given ca
func CreateSignedServingPair(notAfter time.Time, organization string, ca *KeyPair, hosts []string) (*KeyPair, error) {
	notBefore := time.Now()
	if notAfter.Before(notBefore) {
		return nil, fmt.Errorf("invalid notAfter: %s before %s", notAfter.String(), notBefore.String())
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).SetInt64(math.MaxInt64))
	if err != nil {
		return nil, err
	}

	certDetails := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{organization},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		DNSNames:              hosts,
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	publicKey := &privateKey.PublicKey
	certRaw, err := x509.CreateCertificate(rand.Reader, certDetails, ca.Cert, publicKey, ca.Priv)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(certRaw)
	if err != nil {
		return nil, err
	}

	servingCert := &KeyPair{
		Cert: cert,
		Priv: privateKey,
	}

	return servingCert, nil
}

// PEMToCert converts the PEM block of the given byte array to an x509 certificate
func PEMToCert(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("cert PEM empty")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	return cert, nil
}

// VerifyCert checks that the given cert is signed and trusted by the given CA
func VerifyCert(ca, cert *x509.Certificate, host string) error {
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	opts := x509.VerifyOptions{
		DNSName: host,
		Roots:   roots,
	}

	if _, err := cert.Verify(opts); err != nil {
		return err
	}

	return nil
}

// Active checks if the given cert is within its valid time window
func Active(cert *x509.Certificate) bool {
	now := time.Now()
	active := now.After(cert.NotBefore) && now.Before(cert.NotAfter)
	return active
}

// PEMHash returns a hash of the given PEM encoded cert
type PEMHash func(certPEM []byte) (hash string)

// PEMSHA256 returns the hex encoded SHA 256 hash of the given PEM encoded cert
func PEMSHA256(certPEM []byte) (hash string) {
	hasher := sha256.New()
	hasher.Write(certPEM)
	hash = hex.EncodeToString(hasher.Sum(nil))
	return
}
