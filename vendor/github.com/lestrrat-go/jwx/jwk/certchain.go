package jwk

import (
	"crypto/x509"
	"encoding/base64"

	"github.com/pkg/errors"
)

func (c CertificateChain) Get() []*x509.Certificate {
	return c.certs
}

func (c *CertificateChain) Accept(v interface{}) error {
	switch x := v.(type) {
	case string:
		return c.Accept([]string{x})
	case []interface{}:
		l := make([]string, len(x))
		for i, e := range x {
			if es, ok := e.(string); ok {
				l[i] = es
			} else {
				return errors.Errorf(`invalid list element type: expected string, got %T`, v)
			}
		}
		return c.Accept(l)
	case []string:
		certs := make([]*x509.Certificate, len(x))
		for i, e := range x {
			buf, err := base64.StdEncoding.DecodeString(e)
			if err != nil {
				return errors.Wrap(err, `failed to base64 decode list element`)
			}
			cert, err := x509.ParseCertificate(buf)
			if err != nil {
				return errors.Wrap(err, `failed to parse certificate`)
			}
			certs[i] = cert
		}

		*c = CertificateChain{
			certs: certs,
		}
		return nil
	default:
		return errors.Errorf(`invalid value %T`, v)
	}
}
