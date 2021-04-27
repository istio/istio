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
	"regexp"
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
	spiffePattern    = regexp.MustCompile(`spiffe://([^/]+)/.*$`)
	trustDomain      = defaultTrustDomain
	trustDomainMutex sync.RWMutex
)

func SetTrustDomain(value string) {
	// Replace special characters in spiffe
	v := strings.Replace(value, "@", ".", -1)
	trustDomainMutex.Lock()
	trustDomain = v
	trustDomainMutex.Unlock()
}

func GetTrustDomain() string {
	trustDomainMutex.RLock()
	defer trustDomainMutex.RUnlock()
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

// GenSpiffeURI returns the formatted uri(SPIFFE format for now) for the certificate.
func GenSpiffeURI(ns, serviceAccount string) (string, error) {
	var err error
	if ns == "" || serviceAccount == "" {
		err = fmt.Errorf(
			"namespace or service account empty for SPIFFE uri ns=%v serviceAccount=%v", ns, serviceAccount)
	}
	return URIPrefix + GetTrustDomain() + "/ns/" + ns + "/sa/" + serviceAccount, err
}

// MustGenSpiffeURI returns the formatted uri(SPIFFE format for now) for the certificate and logs if there was an error.
func MustGenSpiffeURI(ns, serviceAccount string) string {
	uri, err := GenSpiffeURI(ns, serviceAccount)
	if err != nil {
		log.Debug(err.Error())
	}
	return uri
}

// ExpandWithTrustDomains expands a given spiffe identities, plus a list of truts domain aliases.
// We ensure the returned list does not contain duplicates; the original input is always retained.
// For example,
// ExpandWithTrustDomains({"spiffe://td1/ns/def/sa/def"}, {"td1", "td2"}) returns
//   {"spiffe://td1/ns/def/sa/def", "spiffe://td2/ns/def/sa/def"}.
// ExpandWithTrustDomains({"spiffe://td1/ns/def/sa/a", "spiffe://td1/ns/def/sa/b"}, {"td2"}) returns
//   {"spiffe://td1/ns/def/sa/a", "spiffe://td2/ns/def/sa/a", "spiffe://td1/ns/def/sa/b", "spiffe://td2/ns/def/sa/b"}.
func ExpandWithTrustDomains(spiffeIdentities, trustDomainAliases []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, id := range spiffeIdentities {
		out[id] = struct{}{}
		// Expand with aliases set.
		m := spiffePattern.FindStringSubmatchIndex(id)
		// FindStringSubmatchIndex the pairs of the match, (trust domain + the whole match string) x 2
		// so we should see 4 index.
		if len(m) < 4 {
			log.Errorf("Failed to extract SPIFFE trust domain from: %v, match %v", id, m)
			continue
		}
		suffix := id[m[3]:]
		for _, td := range trustDomainAliases {
			nid := URIPrefix + td + suffix
			out[nid] = struct{}{}
		}
	}
	return out
}

// GenCustomSpiffe returns the  spiffe string that can have a custom structure
func GenCustomSpiffe(identity string) string {
	if identity == "" {
		log.Error("spiffe identity can't be empty")
		return ""
	}

	return URIPrefix + GetTrustDomain() + "/" + identity
}
