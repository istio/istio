// Copyright Istio Authors
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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"gopkg.in/square/go-jose.v2"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/config/constants"
)

const (
	Scheme = "spiffe"

	URIPrefix = Scheme + "://"

	// The default SPIFFE URL value for trust domain
	defaultTrustDomain = constants.DefaultKubernetesDomain
)

var (
	trustDomain      = defaultTrustDomain
	trustDomainMutex sync.RWMutex
)

type bundleDoc struct {
	jose.JSONWebKeySet
	Sequence    uint64 `json:"spiffe_sequence,omitempty"`
	RefreshHint int    `json:"spiffe_refresh_hint,omitempty"`
}

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

// GenCustomSpiffe returns the  spiffe string that can have a custom structure
func GenCustomSpiffe(identity string) string {
	if identity == "" {
		log.Error("spiffe identity can't be empty")
		return ""
	}

	return URIPrefix + GetTrustDomain() + "/" + identity
}

// RetrieveSpiffeBundleRootCert retrieves the trusted CA certificate from a SPIFFE bundle endpoint.
// It can use the system cert pool and the supplied certificates to validate the endpoint.
func RetrieveSpiffeBundleRootCert(endpoint string, extraTrustedCerts []*x509.Certificate) (*x509.Certificate, error) {
	httpClient := &http.Client{}
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("Failed to get SystemCertPool: %v", err)
	}
	for _, cert := range extraTrustedCerts {
		caCertPool.AddCert(cert)
	}

	if !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("Failed to split the SPIFFE endpoint: %v", err)
	}

	config := &tls.Config{
		ServerName: u.Hostname(),
		RootCAs:    caCertPool,
	}

	httpClient.Transport = &http.Transport{
		TLSClientConfig: config,
	}

	resp, err := httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bundle: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b := make([]byte, 1024)
		n, _ := resp.Body.Read(b)
		return nil, fmt.Errorf("unexpected status %d fetching bundle: %s", resp.StatusCode, string(b[:n]))
	}

	doc := new(bundleDoc)
	if err := json.NewDecoder(resp.Body).Decode(doc); err != nil {
		return nil, fmt.Errorf("failed to decode bundle: %v", err)
	}

	for i, key := range doc.Keys {
		if key.Use == "x509-svid" {
			if len(key.Certificates) != 1 {
				return nil, fmt.Errorf("expected a single certificate in x509-svid entry %d; got %d", i, len(key.Certificates))
			}
			return key.Certificates[0], nil
		}
	}
	return nil, fmt.Errorf("the SPIFFE bundle at %s does not have an X509 SVID field", endpoint)
}
