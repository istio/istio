package spiffe

import (
	"crypto/x509"
	"fmt"

	"github.com/spiffe/go-spiffe/uri"
)

// Tries to match a SPIFFE ID, given a certificate
func MatchID(ids []string, cert *x509.Certificate) error {
	parsedIDs, err := uri.GetURINamesFromCertificate(cert)
	if err != nil {
		return err
	}

	for _, parsedID := range parsedIDs {
		for _, id := range ids {
			if parsedID == id {
				return nil
			}
		}
	}

	return fmt.Errorf("SPIFFE ID mismatch")
}

// Verify a SPIFFE certificate and its certification path
func VerifyCertificate(leaf *x509.Certificate, intermediates *x509.CertPool, roots *x509.CertPool) error {
	verifyOpts := x509.VerifyOptions{
		Intermediates: intermediates,
		Roots: roots,
	}

	// TODO: SPIFFE-specific validation of leaf and verified chain
	_, err := leaf.Verify(verifyOpts)
	return err
}
