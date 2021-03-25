package main

import (
	"crypto/tls"

	"k8s.io/apimachinery/pkg/util/sets"

	"istio.io/istio/pilot/pkg/bootstrap"
)

// insecureTLSCipherNames returns a list of cipher suite names implemented by crypto/tls
// which have security issues.
func insecureTLSCipherNames() []string {
	cipherKeys := sets.NewString()
	for _, cipher := range tls.InsecureCipherSuites() {
		cipherKeys.Insert(cipher.Name)
	}
	return cipherKeys.List()
}

// secureTLSCipherNames returns a list of cipher suite names implemented by crypto/tls.
func secureTLSCipherNames() []string {
	cipherKeys := sets.NewString()
	for _, cipher := range tls.CipherSuites() {
		cipherKeys.Insert(cipher.Name)
	}
	return cipherKeys.List()
}

func ValidateFlags(serverArgs *bootstrap.PilotArgs) error {
	if serverArgs == nil {
		return nil
	}
	_, err := bootstrap.TLSCipherSuites(serverArgs.ServerOptions.TLSOptions.TLSCipherSuites)
	// TODO: add validation for other flags

	return err
}
