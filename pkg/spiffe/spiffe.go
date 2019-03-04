package spiffe

import (
	"fmt"
	"strings"
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
func GenSpiffeURI(ns, serviceAccount string) string {
	// replace special character in spiffe
	trustDomain = strings.Replace(trustDomain, "@", ".", -1)
	return fmt.Sprintf(Scheme+"://%s/ns/%s/sa/%s", trustDomain, ns, serviceAccount)
}
