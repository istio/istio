// Copyright 2019 Istio Authors
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
	"strings"
	"sync"

	"istio.io/pkg/log"
)

const (
	Scheme = "spiffe"

	URIPrefix = Scheme + "://"

	// The default SPIFFE URL value for trust domain
	defaultTrustDomain = "cluster.local"
)

var (
	trustDomain             = defaultTrustDomain
	trustDomainAliases      = []string{}
	trustDomainMutex        sync.RWMutex
	trustDomainAliasesMutex sync.RWMutex
)

func SetTrustDomain(value string) {
	// Replace special characters in spiffe
	v := strings.Replace(value, "@", ".", -1)
	trustDomainMutex.Lock()
	trustDomain = v
	trustDomainMutex.Unlock()
}

func SetTrustDomainAliases(aliases []string) {
	for _, alias := range aliases {
		trustDomainAliasesMutex.Lock()
		trustDomainAliases = append(trustDomainAliases, alias)
		trustDomainAliasesMutex.Unlock()
	}
}

func GetTrustDomain() string {
	trustDomainMutex.RLock()
	defer trustDomainMutex.RUnlock()
	return trustDomain
}

func GetTrustDomainAliases() []string {
	trustDomainAliasesMutex.RLock()
	defer trustDomainAliasesMutex.RUnlock()
	return trustDomainAliases
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

// GenSpiffeURI returns the formatted uri(SPIFFE format for now) for the certificate.
// If the trust domain parameter is empty, the trust domain will be determined by the GetTrustDomain
// function, which returns a default trust domain or a user-provided trust domain.
func GenSpiffeURI(td, ns, serviceAccount string) (string, error) {
	var err error
	var trustDomain string
	if td != "" {
		trustDomain = td
	} else {
		trustDomain = GetTrustDomain()
	}
	if ns == "" || serviceAccount == "" {
		err = fmt.Errorf(
			"namespace or service account empty for SPIFFE uri ns=%v serviceAccount=%v", ns, serviceAccount)
	}
	return URIPrefix + trustDomain + "/ns/" + ns + "/sa/" + serviceAccount, err
}

// MustGenSpiffeURI returns the formatted uri(SPIFFE format for now) for the certificate and logs if there was an error.
// If the trust domain parameter is empty, the trust domain will be determined by the GetTrustDomain
// function, which returns a default trust domain or a user-provided trust domain.
func MustGenSpiffeURI(td, ns, serviceAccount string) string {
	uri, err := GenSpiffeURI(td, ns, serviceAccount)
	if err != nil {
		log.Debug(err.Error())
	}
	return uri
}

// GenCustomSpiffe returns the  spiffe string that can have a custom structure
func GenCustomSpiffe(identity string) string {
	if identity == "" {
		log.Error("spiffe identity can't be empty")
		return ""
	}

	return URIPrefix + GetTrustDomain() + "/" + identity
}
