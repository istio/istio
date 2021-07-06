package util

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"istio.io/istio/pilot/pkg/keycertbundle"
)

type ConfigError struct {
	err    error
	reason string
}

func (e ConfigError) Error() string {
	return e.err.Error()
}

func (e ConfigError) Reason() string {
	return e.reason
}

func LoadCABundle(caBundleWatcher *keycertbundle.Watcher) ([]byte, error) {
	caBundle := caBundleWatcher.GetCABundle()
	if err := VerifyCABundle(caBundle); err != nil {
		return nil, &ConfigError{err, "could not verify caBundle"}
	}

	return caBundle, nil
}

func VerifyCABundle(caBundle []byte) error {
	block, _ := pem.Decode(caBundle)
	if block == nil {
		return errors.New("could not decode pem")
	}
	if block.Type != "CERTIFICATE" {
		return fmt.Errorf("cert contains wrong pem type: %q", block.Type)
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return fmt.Errorf("cert contains invalid x509 certificate: %v", err)
	}
	return nil
}
