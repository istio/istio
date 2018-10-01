// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
	globalIdentityDomain = determineIdentityDomain(identityDomain, domain, isKubernetes)
	return globalIdentityDomain
}

func determineIdentityDomain(identityDomain string, domain string, isKubernetes bool) string {

	if len(identityDomain) != 0 {
		return identityDomain
	}
	envIdentityDomain := os.Getenv("ISTIO_SA_DOMAIN_CANONICAL")
	if len(envIdentityDomain) > 0 {
		return envIdentityDomain
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
	if globalIdentityDomain == "" {
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
