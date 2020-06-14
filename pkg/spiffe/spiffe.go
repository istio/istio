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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/spiffe/spire/pkg/common/bundleutil"
	"github.com/spiffe/spire/pkg/common/idutil"
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

// RetrieveSpiffeBundleRootCerts retrieves the trusted CA certificates from a SPIFFE bundle endpoint.
// It can use the system cert pool and the supplied certificates to validate the endpoint.
func RetrieveSpiffeBundleRootCerts(endpoint string, extraTrustedCerts []*x509.Certificate) ([]*x509.Certificate, error) {
	httpClient := &http.Client{}
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("Failed to get SystemCertPool: %v", err)
	}
	for _, cert := range extraTrustedCerts {
		caCertPool.AddCert(cert)
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

	if !strings.HasPrefix(endpoint, "https://") {
		endpoint = "https://" + endpoint
	}
	resp, err := httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bundle: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching bundle: %s", resp.StatusCode, tryRead(resp.Body))
	}

	b, err := bundleutil.Decode(idutil.TrustDomainID(""), resp.Body)
	if err != nil {
		return nil, err
	}

	return b.RootCAs(), nil
}

func tryRead(r io.Reader) string {
	b := make([]byte, 1024)
	n, _ := r.Read(b)
	return string(b[:n])
}
