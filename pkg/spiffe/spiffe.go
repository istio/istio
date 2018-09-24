package spiffe

import (
	"fmt"
	"os"
)

const (
	Scheme = "spiffe"
)

var globalIdentityDomain = ""

func SetIdentityDomain(identityDomain string, domain string, isKubernetes bool) string {
	globalIdentityDomain = determineIdentityDomain(identityDomain,domain,isKubernetes)
	return globalIdentityDomain
}

func determineIdentityDomain(identityDomain string, domain string, isKubernetes bool) string {

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
	if globalIdentityDomain =="" {
		return "", fmt.Errorf(
			"identity domain can't be empty. Please use SetIdentityDomain to configure the identity domain")

	}
	if ns == "" || serviceAccount == "" {
		return "", fmt.Errorf(
			"namespace or service account can't be empty ns=%v serviceAccount=%v", ns, serviceAccount)
	}
	return fmt.Sprintf(Scheme+"://%s/ns/%s/sa/%s", globalIdentityDomain, ns, serviceAccount), nil
}

func MustGenSpiffeURI(ns, serviceAccount string) string {
	uri, err := GenSpiffeURI(ns, serviceAccount)
	if err != nil {
		panic(err)
	}
	return uri
}
