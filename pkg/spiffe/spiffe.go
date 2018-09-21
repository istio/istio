package spiffe

import (
	"fmt"
	"os"
)

const (
	Scheme = "spiffe"
)

var identityDomain = ""

func SetIdentityDomain(value string) {
	identityDomain = value
}

func GetIdentityDomain() string {
	return identityDomain
}

func DetermineIdentityDomain(identityDomain string, domain string, isKubernetes bool) string {

	if len(identityDomain) != 0 {
		return identityDomain
	}
	envTrustDomain := os.Getenv("ISTIO_SA_DOMAIN_CANONICAL")
	if len(envTrustDomain) > 0 {
		return envTrustDomain
	}
	if len(domain) != 0 {
		return domain
	}
	if isKubernetes {
		return "cluster.local"
	} else {
		return domain
	}
}

// GenSpiffeURI returns the formatted uri(SPIFFEE format for now) for the certificate.
func GenSpiffeURI(ns, serviceAccount string) (string, error) {
	if ns == "" || serviceAccount == "" {
		return "", fmt.Errorf(
			"namespace or service account can't be empty ns=%v serviceAccount=%v", ns, serviceAccount)
	}
	return fmt.Sprintf(Scheme+"://%s/ns/%s/sa/%s", identityDomain, ns, serviceAccount), nil
}

func MustGenSpiffeURI(ns, serviceAccount string) string {
	uri, err := GenSpiffeURI(ns, serviceAccount)
	if err != nil {
		panic(err)
	}
	return uri
}
