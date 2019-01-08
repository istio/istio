package spiffe

import (
	"fmt"
	"istio.io/istio/pkg/log"
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
	var err error
	if ns == "" || serviceAccount == "" {
		err = fmt.Errorf(
			"namespace or service account can't be empty ns=%v serviceAccount=%v", ns, serviceAccount)
	}
	return fmt.Sprintf(Scheme+"://%s/ns/%s/sa/%s", trustDomain, ns, serviceAccount), err
}

// MustGenSpiffeURI returns the formatted uri(SPIFFEE format for now) for the certificate and logs if there was an error.
func MustGenSpiffeURI(ns, serviceAccount string) string {
	uri, err := GenSpiffeURI(ns, serviceAccount)
	if err != nil {
		log.Error(err.Error())
	}
	return uri
}
