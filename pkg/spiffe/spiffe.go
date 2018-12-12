package spiffe

import (
	"fmt"
	"os"
)

const (
	Scheme = "spiffe"

	// The default SPIFFE URL value for trust domain
	defaultTrustDomain = "cluster.local"
)

var trustDomain = defaultTrustDomain

func SetTrustDomain(value string) {
	trustDomain = value
}

func GetTrustDomain() string {
	return trustDomain
}

func DetermineTrustDomain(commandLineTrustDomain string, domain string, isKubernetes bool) string {

	if len(commandLineTrustDomain) != 0 {
		return commandLineTrustDomain
	}
	envTrustDomain := os.Getenv("ISTIO_SA_DOMAIN_CANONICAL")
	if len(envTrustDomain) > 0 {
		return envTrustDomain
	}
	if len(domain) != 0 {
		return domain
	}
	if isKubernetes {
		return defaultTrustDomain
	}
	return domain
}

// GenSpiffeURI returns the formatted uri(SPIFFEE format for now) for the certificate.
func GenSpiffeURI(ns, serviceAccount string) (string, error) {
	if ns == "" || serviceAccount == "" {
		return "", fmt.Errorf(
			"namespace or service account can't be empty ns=%v serviceAccount=%v", ns, serviceAccount)
	}
	return fmt.Sprintf(Scheme+"://%s/ns/%s/sa/%s", trustDomain, ns, serviceAccount), nil
}

// MustGenSpiffeURI returns the formatted uri(SPIFFEE format for now) for the certificate and panics if there was an error.
func MustGenSpiffeURI(ns, serviceAccount string) string {
	uri, err := GenSpiffeURI(ns, serviceAccount)
	if err != nil {
		panic(err)
	}
	return uri
}
