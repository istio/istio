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

package trustbundle

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sort"
	"sync"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

// Source is all possible sources of MeshConfig
type Source int

// Type aliases for convenience
type (
	certString  = string
	endpoint    = string
	trustDomain = string
)

// Used to indicate any trust domain obtained by
// spiffe.GetTrustDomain(). This value stays the same
// even if the default trust domain is updated by users
// in the middle of trust anchor updates.
const defaultTrustDomain = "default"

type trustDomainCerts = map[trustDomain][]certString

type TrustBundle struct {
	sourceToTrustDomainCerts map[Source]trustDomainCerts
	mutex                    sync.RWMutex
	mergedTrustDomainBundles map[trustDomain][]certString
	updatecb                 func()
	endpointMutex            sync.RWMutex
	endpoints                map[endpoint][]trustDomain
	endpointUpdateChan       chan struct{}
	remoteCaCertPool         *x509.CertPool
}

var (
	trustBundleLog               = log.RegisterScope("trustBundle", "Workload mTLS trust bundle logs", 0)
	remoteTimeout  time.Duration = 10 * time.Second
)

const (
	SourceIstioCA Source = iota
	SourceMeshConfig
	SourceIstioRA
	sourceSpiffeEndpoints

	RemoteDefaultPollPeriod = 30 * time.Minute
)

func isEqStrToSliceStrMap(x map[string][]string, y map[string][]string) bool {
	if len(x) != len(y) {
		return false
	}
	for trustDomain, certs1 := range x {
		certs2, ok := y[trustDomain]
		if !ok {
			return false
		}
		if len(certs1) != len(certs2) {
			return false
		}
		for i := range certs1 {
			if certs1[i] != certs2[i] {
				return false
			}
		}
	}
	return true
}

// NewTrustBundle returns a new trustbundle
func NewTrustBundle(remoteCaCertPool *x509.CertPool) *TrustBundle {
	spiffe.GetTrustDomain()
	var err error
	tb := &TrustBundle{
		sourceToTrustDomainCerts: map[Source]trustDomainCerts{
			SourceIstioCA:         map[trustDomain][]certString{},
			SourceMeshConfig:      map[trustDomain][]certString{},
			SourceIstioRA:         map[trustDomain][]certString{},
			sourceSpiffeEndpoints: map[trustDomain][]certString{},
		},
		mergedTrustDomainBundles: map[trustDomain][]certString{},
		updatecb:                 nil,
		endpointUpdateChan:       make(chan struct{}, 1),
		endpoints:                map[string][]trustDomain{},
	}
	if remoteCaCertPool == nil {
		tb.remoteCaCertPool, err = x509.SystemCertPool()
		if err != nil {
			trustBundleLog.Errorf("failed to initialize remote Cert pool: %v", err)
		}
	} else {
		tb.remoteCaCertPool = remoteCaCertPool
	}
	return tb
}

func (tb *TrustBundle) UpdateCb(updatecb func()) {
	tb.updatecb = updatecb
}

// GetTrustBundle : Retrieves all the trustAnchors for current Spiffee Trust Domain
// TODO: support non-default trust domains.
func (tb *TrustBundle) GetTrustBundle() []certString {
	tb.mutex.RLock()
	defer tb.mutex.RUnlock()
	certs := tb.mergedTrustDomainBundles[defaultTrustDomain]
	trustedCerts := make([]certString, len(certs))
	copy(trustedCerts, certs)
	return trustedCerts
}

func verifyTrustAnchor(trustAnchor string) error {
	block, _ := pem.Decode([]byte(trustAnchor))
	if block == nil {
		return fmt.Errorf("failed to decode pem certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse X.509 certificate: %v", err)
	}
	if !cert.IsCA {
		return fmt.Errorf("certificate is not a CA certificate")
	}
	return nil
}

func (tb *TrustBundle) mergeInternal() {
	var ok bool
	mergedCerts := trustDomainCerts{}

	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	for _, trustDomainCerts := range tb.sourceToTrustDomainCerts {
		certMap := make(map[certString]struct{})
		for trustDomain, certs := range trustDomainCerts {
			for _, cert := range certs {
				if _, ok = certMap[cert]; !ok {
					certMap[cert] = struct{}{}
					mergedCerts[trustDomain] = append(mergedCerts[trustDomain], cert)
				}
			}
		}
	}
	tb.mergedTrustDomainBundles = mergedCerts
	for trustDomain := range mergedCerts {
		sort.Strings(mergedCerts[trustDomain])
	}
}

func (tb *TrustBundle) AddTrustAnchorInDefaultTrustDomain(source Source, cert string) error {
	return tb.updateTrustAnchor(source, map[trustDomain][]certString{defaultTrustDomain: {cert}})
}

// UpdateTrustAnchor : External Function to merge a TrustAnchor config with the existing TrustBundle
func (tb *TrustBundle) updateTrustAnchor(source Source, trustDomainCerts trustDomainCerts) error {
	var ok bool
	var err error

	tb.mutex.RLock()
	cachedCerts, ok := tb.sourceToTrustDomainCerts[source]
	tb.mutex.RUnlock()
	if !ok {
		return fmt.Errorf("invalid source of TrustBundle configuration %v", source)
	}

	// Check if anything needs to be changed at all
	if isEqStrToSliceStrMap(trustDomainCerts, cachedCerts) {
		trustBundleLog.Debugf("no change to trustAnchor configuration after recent update")
		return nil
	}

	for _, certs := range trustDomainCerts {
		for _, cert := range certs {
			err = verifyTrustAnchor(cert)
			if err != nil {
				return err
			}
		}
	}
	tb.mutex.Lock()
	tb.sourceToTrustDomainCerts[source] = trustDomainCerts
	tb.mutex.Unlock()
	tb.mergeInternal()

	trustBundleLog.Infof("updating Source %v with certs %v\n",
		source, trustDomainCerts)

	if tb.updatecb != nil {
		tb.updatecb()
	}
	return nil
}

func (tb *TrustBundle) updateRemoteEndpoint(spiffeEndpoints map[endpoint][]trustDomain) {
	tb.mutex.RLock()
	remoteEndpoints := tb.endpoints
	tb.mutex.RUnlock()

	if isEqStrToSliceStrMap(spiffeEndpoints, remoteEndpoints) {
		return
	}
	trustBundleLog.Infof("updated remote endpoints  :%v", spiffeEndpoints)
	tb.mutex.Lock()
	tb.endpoints = spiffeEndpoints
	tb.mutex.Unlock()
	tb.endpointUpdateChan <- struct{}{}
}

// AddMeshConfigUpdate : Update trustAnchor configurations from meshConfig
func (tb *TrustBundle) AddMeshConfigUpdate(cfg *meshconfig.MeshConfig) error {
	var err error
	if cfg != nil {
		certs := map[trustDomain][]certString{}
		endpoints := map[endpoint][]trustDomain{}
		for _, pemCert := range cfg.GetCaCertificates() {
			trustDomains := []trustDomain{defaultTrustDomain}
			if len(pemCert.TrustDomains) > 0 {
				trustDomains = pemCert.TrustDomains
			}
			cert := pemCert.GetPem()
			if cert != "" {
				for _, trustDomain := range trustDomains {
					certs[trustDomain] = append(certs[trustDomain], cert)
				}
				fmt.Println(certs)
			} else if pemCert.GetSpiffeBundleUrl() != "" {
				endpoints[pemCert.GetSpiffeBundleUrl()] = trustDomains
			}
		}

		err = tb.updateTrustAnchor(SourceMeshConfig, certs)
		if err != nil {
			trustBundleLog.Errorf("failed to update meshConfig PEM trustAnchors: %v", err)
			return err
		}

		tb.updateRemoteEndpoint(endpoints)
	}
	return nil
}

func (tb *TrustBundle) fetchRemoteTrustAnchors() {
	var err error

	tb.endpointMutex.RLock()
	remoteEndpoints := tb.endpoints
	tb.endpointMutex.RUnlock()
	remoteCerts := map[trustDomain][]certString{}

	for endpoint, trustDomains := range remoteEndpoints {
		cert, err := spiffe.RetrieveSpiffeBundleRootCert(endpoint, tb.remoteCaCertPool, remoteTimeout)
		if err != nil {
			trustBundleLog.Errorf("unable to fetch trust Anchors from %s: %s", endpoint, err)
			continue
		}
		certStr := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
		trustBundleLog.Debugf("from endpoint %v, fetched trust anchor cert: %v", endpoint, certStr)
		for _, trustDomain := range trustDomains {
			remoteCerts[trustDomain] = append(remoteCerts[trustDomain], certStr)
		}
	}
	err = tb.updateTrustAnchor(sourceSpiffeEndpoints, remoteCerts)
	if err != nil {
		trustBundleLog.Errorf("failed to update meshConfig Spiffe trustAnchors: %v", err)
	}
}

func (tb *TrustBundle) ProcessRemoteTrustAnchors(stop <-chan struct{}, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			trustBundleLog.Infof("waking up to perform periodic checks")
			tb.fetchRemoteTrustAnchors()
		case <-stop:
			trustBundleLog.Infof("stop processing endpoint trustAnchor updates")
			return
		case <-tb.endpointUpdateChan:
			tb.fetchRemoteTrustAnchors()
			trustBundleLog.Infof("processing endpoint trustAnchor Updates for config change")
		}
	}
}
