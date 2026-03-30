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
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

const (
	Scheme = "spiffe"

	URIPrefix    = Scheme + "://"
	URIPrefixLen = len(URIPrefix)

	// The default SPIFFE URL value for trust domain
	defaultTrustDomain = constants.DefaultClusterLocalDomain

	ServiceAccountSegment = "sa"
	NamespaceSegment      = "ns"
)

var (
	firstRetryBackOffTime = time.Millisecond * 50

	spiffeLog = log.RegisterScope("spiffe", "SPIFFE library logging")
)

type Identity struct {
	TrustDomain    string
	Namespace      string
	ServiceAccount string
}

func ParseIdentity(s string) (Identity, error) {
	if !strings.HasPrefix(s, URIPrefix) {
		return Identity{}, fmt.Errorf("identity is not a spiffe format")
	}
	split := strings.Split(s[URIPrefixLen:], "/")
	if len(split) != 5 {
		return Identity{}, fmt.Errorf("identity is not a spiffe format")
	}
	if split[1] != NamespaceSegment || split[3] != ServiceAccountSegment {
		return Identity{}, fmt.Errorf("identity is not a spiffe format")
	}
	return Identity{
		TrustDomain:    split[0],
		Namespace:      split[2],
		ServiceAccount: split[4],
	}, nil
}

func (i Identity) String() string {
	return URIPrefix + i.TrustDomain + "/ns/" + i.Namespace + "/sa/" + i.ServiceAccount
}

type bundleDoc struct {
	jose.JSONWebKeySet
	Sequence    uint64 `json:"spiffe_sequence,omitempty"`
	RefreshHint int    `json:"spiffe_refresh_hint,omitempty"`
}

func sanitizeTrustDomain(td string) string {
	return strings.Replace(td, "@", ".", -1)
}

// GenSpiffeURI returns the formatted uri(SPIFFE format for now) for the certificate.
func genSpiffeURI(td, ns, serviceAccount string) (string, error) {
	var err error
	if ns == "" || serviceAccount == "" {
		err = fmt.Errorf(
			"namespace or service account empty for SPIFFE uri ns=%v serviceAccount=%v", ns, serviceAccount)
	}
	return URIPrefix + sanitizeTrustDomain(td) + "/ns/" + ns + "/sa/" + serviceAccount, err
}

// MustGenSpiffeURI returns the formatted uri(SPIFFE format for now) for the certificate and logs if there was an error.
func MustGenSpiffeURI(meshCfg *meshconfig.MeshConfig, ns, serviceAccount string) string {
	uri, err := genSpiffeURI(meshCfg.GetTrustDomain(), ns, serviceAccount)
	if err != nil {
		spiffeLog.Debug(err.Error())
	}
	return uri
}

// MustGenSpiffeURIForTrustDomain returns the formatted uri(SPIFFE format for now) for the certificate and logs if there was an error.
func MustGenSpiffeURIForTrustDomain(td, ns, serviceAccount string) string {
	uri, err := genSpiffeURI(td, ns, serviceAccount)
	if err != nil {
		spiffeLog.Debug(err.Error())
	}
	return uri
}

// ExpandWithTrustDomains expands a given spiffe identities, plus a list of trust domain aliases.
// We ensure the returned list does not contain duplicates; the original input is always retained.
// For example,
// ExpandWithTrustDomains({"spiffe://td1/ns/def/sa/def"}, {"td1", "td2"}) returns
//
//	{"spiffe://td1/ns/def/sa/def", "spiffe://td2/ns/def/sa/def"}.
//
// ExpandWithTrustDomains({"spiffe://td1/ns/def/sa/a", "spiffe://td1/ns/def/sa/b"}, {"td2"}) returns
//
//	{"spiffe://td1/ns/def/sa/a", "spiffe://td2/ns/def/sa/a", "spiffe://td1/ns/def/sa/b", "spiffe://td2/ns/def/sa/b"}.
func ExpandWithTrustDomains(spiffeIdentities sets.String, trustDomainAliases []string) sets.String {
	if len(trustDomainAliases) == 0 {
		return spiffeIdentities
	}
	out := sets.New[string]()
	for id := range spiffeIdentities {
		out.Insert(id)
		// Skip if not a SPIFFE identity - This can happen for example if the identity is a DNS name.
		if !strings.HasPrefix(id, URIPrefix) {
			continue
		}
		// Expand with aliases set.
		m, err := ParseIdentity(id)
		if err != nil {
			spiffeLog.Errorf("Failed to extract SPIFFE trust domain from %v: %v", id, err)
			continue
		}
		for _, td := range trustDomainAliases {
			m.TrustDomain = td
			out.Insert(m.String())
		}
	}
	return out
}

// GetTrustDomainFromURISAN extracts the trust domain part from the URI SAN in the X.509 certificate.
func GetTrustDomainFromURISAN(uriSan string) (string, error) {
	parsed, err := ParseIdentity(uriSan)
	if err != nil {
		return "", fmt.Errorf("failed to parse URI SAN %s. Error: %v", uriSan, err)
	}
	return parsed.TrustDomain, nil
}

// RetrieveSpiffeBundleRootCerts retrieves the trusted CA certificates from a list of SPIFFE bundle endpoints.
// It can use the system cert pool and the supplied certificates to validate the endpoints.
func RetrieveSpiffeBundleRootCerts(config map[string]string, caCertPool *x509.CertPool, retryTimeout time.Duration) (
	map[string][]*x509.Certificate, error,
) {
	httpClient := &http.Client{
		Timeout: time.Second * 10,
	}

	ret := map[string][]*x509.Certificate{}
	for trustDomain, endpoint := range config {
		if !strings.HasPrefix(endpoint, "https://") {
			endpoint = "https://" + endpoint
		}
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to split the SPIFFE bundle URL: %v", err)
		}

		config := &tls.Config{
			ServerName: u.Hostname(),
			RootCAs:    caCertPool,
			MinVersion: tls.VersionTLS12,
		}

		httpClient.Transport = &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: config,
			DialContext: (&net.Dialer{
				Timeout: time.Second * 10,
			}).DialContext,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}

		retryBackoffTime := firstRetryBackOffTime
		startTime := time.Now()
		var resp *http.Response
		for {
			resp, err = httpClient.Get(endpoint)
			var errMsg string
			if err != nil {
				errMsg = fmt.Sprintf("Calling %s failed with error: %v", endpoint, err)
			} else if resp == nil {
				errMsg = fmt.Sprintf("Calling %s failed with nil response", endpoint)
			} else if resp.StatusCode != http.StatusOK {
				b := make([]byte, 1024)
				n, _ := resp.Body.Read(b)
				errMsg = fmt.Sprintf("Calling %s failed with unexpected status: %v, fetching bundle: %s",
					endpoint, resp.StatusCode, string(b[:n]))
			} else {
				break
			}

			if startTime.Add(retryTimeout).Before(time.Now()) {
				return nil, fmt.Errorf("exhausted retries to fetch the SPIFFE bundle %s from url %s. Latest error: %v",
					trustDomain, endpoint, errMsg)
			}

			spiffeLog.Warnf("%s, retry in %v", errMsg, retryBackoffTime)
			time.Sleep(retryBackoffTime)
			retryBackoffTime *= 2 // Exponentially increase the retry backoff time.
		}
		defer resp.Body.Close()

		doc := new(bundleDoc)
		if err := json.NewDecoder(resp.Body).Decode(doc); err != nil {
			return nil, fmt.Errorf("trust domain [%s] at URL [%s] failed to decode bundle: %v", trustDomain, endpoint, err)
		}

		var certs []*x509.Certificate
		for i, key := range doc.Keys {
			if key.Use == "x509-svid" {
				if len(key.Certificates) != 1 {
					return nil, fmt.Errorf("trust domain [%s] at URL [%s] expected 1 certificate in x509-svid entry %d; got %d",
						trustDomain, endpoint, i, len(key.Certificates))
				}
				certs = append(certs, key.Certificates[0])
			}
		}
		if len(certs) == 0 {
			return nil, fmt.Errorf("trust domain [%s] at URL [%s] does not provide a X509 SVID", trustDomain, endpoint)
		}
		ret[trustDomain] = certs
	}
	for trustDomain, certs := range ret {
		spiffeLog.Infof("Loaded SPIFFE trust bundle for: %v, containing %d certs", trustDomain, len(certs))
	}
	return ret, nil
}

// PeerCertVerifier is an instance to verify the peer certificate in the SPIFFE way using the retrieved root certificates.
type PeerCertVerifier struct {
	generalCertPool *x509.CertPool
	certPools       map[string]*x509.CertPool
}

// NewPeerCertVerifier returns a new PeerCertVerifier.
func NewPeerCertVerifier() *PeerCertVerifier {
	return &PeerCertVerifier{
		generalCertPool: x509.NewCertPool(),
		certPools:       make(map[string]*x509.CertPool),
	}
}

// GetGeneralCertPool returns generalCertPool containing all root certs.
func (v *PeerCertVerifier) GetGeneralCertPool() *x509.CertPool {
	return v.generalCertPool
}

// AddMapping adds a new trust domain to certificates mapping to the certPools map.
func (v *PeerCertVerifier) AddMapping(trustDomain string, certs []*x509.Certificate) {
	if v.certPools[trustDomain] == nil {
		v.certPools[trustDomain] = x509.NewCertPool()
	}
	for _, cert := range certs {
		v.certPools[trustDomain].AddCert(cert)
		v.generalCertPool.AddCert(cert)
	}
	spiffeLog.Infof("Added %d certs to trust domain %s in peer cert verifier", len(certs), trustDomain)
}

// AddMappingFromPEM adds multiple RootCA's to the spiffe Trust bundle in the trustDomain namespace
func (v *PeerCertVerifier) AddMappingFromPEM(trustDomain string, rootCertBytes []byte) error {
	block, rest := pem.Decode(rootCertBytes)
	var blockBytes []byte

	// Loop while there are no block are found
	for block != nil {
		blockBytes = append(blockBytes, block.Bytes...)
		block, rest = pem.Decode(rest)
	}

	rootCAs, err := x509.ParseCertificates(blockBytes)
	if err != nil {
		spiffeLog.Errorf("parse certificate from rootPEM got error: %v", err)
		return fmt.Errorf("parse certificate from rootPEM got error: %v", err)
	}

	v.AddMapping(trustDomain, rootCAs)
	return nil
}

// AddMappings merges a trust domain to certs map to the certPools map.
func (v *PeerCertVerifier) AddMappings(certMap map[string][]*x509.Certificate) {
	for trustDomain, certs := range certMap {
		v.AddMapping(trustDomain, certs)
	}
}

// VerifyPeerCert is an implementation of tls.Config.VerifyPeerCertificate.
// It verifies the peer certificate using the root certificates associated with its trust domain.
func (v *PeerCertVerifier) VerifyPeerCert(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		// Peer doesn't present a certificate. Just skip. Other authn methods may be used.
		return nil
	}
	var peerCert *x509.Certificate
	intCertPool := x509.NewCertPool()
	for id, rawCert := range rawCerts {
		cert, err := x509.ParseCertificate(rawCert)
		if err != nil {
			return err
		}
		if id == 0 {
			peerCert = cert
		} else {
			intCertPool.AddCert(cert)
		}
	}
	if len(peerCert.URIs) != 1 {
		return fmt.Errorf("peer certificate does not contain 1 URI type SAN, detected %d", len(peerCert.URIs))
	}
	trustDomain, err := GetTrustDomainFromURISAN(peerCert.URIs[0].String())
	if err != nil {
		return err
	}
	rootCertPool, ok := v.certPools[trustDomain]
	if !ok {
		return fmt.Errorf("no cert pool found for trust domain %s", trustDomain)
	}

	_, err = peerCert.Verify(x509.VerifyOptions{
		Roots:         rootCertPool,
		Intermediates: intCertPool,
	})
	return err
}
