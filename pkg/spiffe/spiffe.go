package spiffe

import (
	"fmt"
	"strings"

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

func DetermineTrustDomain(commandLineTrustDomain string, isKubernetes bool) string {

	if len(commandLineTrustDomain) != 0 {
		return commandLineTrustDomain
	}
	if isKubernetes {
		return defaultTrustDomain
	}
	return ""
}

// GenSpiffeURI returns the formatted uri(SPIFFEE format for now) for the certificate.
func GenSpiffeURI(ns, serviceAccount string) (string, error) {
	var err error
	if ns == "" || serviceAccount == "" {
		err = fmt.Errorf(
			"namespace or service account empty for SPIFFEE uri ns=%v serviceAccount=%v", ns, serviceAccount)
	}

	// replace specifial character in spiffe
	trustDomain = strings.Replace(trustDomain, "@", ".", -1)
	return fmt.Sprintf(Scheme+"://%s/ns/%s/sa/%s", trustDomain, ns, serviceAccount), err
}

// MustGenSpiffeURI returns the formatted uri(SPIFFEE format for now) for the certificate and logs if there was an error.
func MustGenSpiffeURI(ns, serviceAccount string) string {
	uri, err := GenSpiffeURI(ns, serviceAccount)
	if err != nil {
		log.Debug(err.Error())
	}
	return uri
}
