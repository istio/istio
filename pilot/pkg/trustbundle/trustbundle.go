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
	"strings"
	"sync"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
)

// Source is all possible sources of MeshConfig
type Source int

type TrustAnchorConfig struct {
	Certs []string
}

type TrustAnchorUpdate struct {
	TrustAnchorConfig
	Source Source
}

type TrustBundle struct {
	sourceConfig       map[Source]TrustAnchorConfig
	mutex              sync.RWMutex
	mergedCerts        []string
	updatecb           func()
	endpointMutex      sync.RWMutex
	endpoints          []string
	endpointUpdateChan chan struct{}
	remoteCaCertPool   *x509.CertPool
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

func isEqSliceStr(certs1 []string, certs2 []string) bool {
	if len(certs1) != len(certs2) {
		return false
	}
	for i := range certs1 {
		if certs1[i] != certs2[i] {
			return false
		}
	}
	return true
}

// NewTrustBundle returns a new trustbundle
func NewTrustBundle(remoteCaCertPool *x509.CertPool) *TrustBundle {
	var err error
	tb := &TrustBundle{
		sourceConfig: map[Source]TrustAnchorConfig{
			SourceIstioCA:         {Certs: []string{}},
			SourceMeshConfig:      {Certs: []string{}},
			SourceIstioRA:         {Certs: []string{}},
			sourceSpiffeEndpoints: {Certs: []string{}},
		},
		mergedCerts:        []string{},
		updatecb:           nil,
		endpointUpdateChan: make(chan struct{}, 1),
		endpoints:          []string{},
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
func (tb *TrustBundle) GetTrustBundle() []string {
	tb.mutex.RLock()
	defer tb.mutex.RUnlock()
	trustedCerts := make([]string, len(tb.mergedCerts))
	copy(trustedCerts, tb.mergedCerts)
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
	var mergeCerts []string
	certMap := sets.New()

	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	for _, configSource := range tb.sourceConfig {
		for _, cert := range configSource.Certs {
			if !certMap.Contains(cert) {
				certMap.Insert(cert)
				mergeCerts = append(mergeCerts, cert)
			}
		}
	}
	tb.mergedCerts = mergeCerts
	sort.Strings(tb.mergedCerts)
}

// UpdateTrustAnchor : External Function to merge a TrustAnchor config with the existing TrustBundle
func (tb *TrustBundle) UpdateTrustAnchor(anchorConfig *TrustAnchorUpdate) error {
	var ok bool
	var err error

	tb.mutex.RLock()
	cachedConfig, ok := tb.sourceConfig[anchorConfig.Source]
	tb.mutex.RUnlock()
	if !ok {
		return fmt.Errorf("invalid source of TrustBundle configuration %v", anchorConfig.Source)
	}

	// Check if anything needs to be changed at all
	if isEqSliceStr(anchorConfig.Certs, cachedConfig.Certs) {
		trustBundleLog.Debugf("no change to trustAnchor configuration after recent update")
		return nil
	}

	for _, cert := range anchorConfig.Certs {
		err = verifyTrustAnchor(cert)
		if err != nil {
			return err
		}
	}
	tb.mutex.Lock()
	tb.sourceConfig[anchorConfig.Source] = anchorConfig.TrustAnchorConfig
	tb.mutex.Unlock()
	tb.mergeInternal()

	trustBundleLog.Infof("updating Source %v with certs %v",
		anchorConfig.Source,
		strings.Join(anchorConfig.TrustAnchorConfig.Certs, "\n"))

	if tb.updatecb != nil {
		tb.updatecb()
	}
	return nil
}

func (tb *TrustBundle) updateRemoteEndpoint(spiffeEndpoints []string) {
	tb.endpointMutex.RLock()
	remoteEndpoints := tb.endpoints
	tb.endpointMutex.RUnlock()

	if isEqSliceStr(spiffeEndpoints, remoteEndpoints) {
		return
	}
	trustBundleLog.Infof("updated remote endpoints  :%v", spiffeEndpoints)
	tb.endpointMutex.Lock()
	tb.endpoints = spiffeEndpoints
	tb.endpointMutex.Unlock()
	tb.endpointUpdateChan <- struct{}{}
}

// AddMeshConfigUpdate : Update trustAnchor configurations from meshConfig
func (tb *TrustBundle) AddMeshConfigUpdate(cfg *meshconfig.MeshConfig) error {
	var err error
	if cfg != nil {
		certs := []string{}
		endpoints := []string{}
		for _, pemCert := range cfg.GetCaCertificates() {
			cert := pemCert.GetPem()
			if cert != "" {
				certs = append(certs, cert)
			} else if pemCert.GetSpiffeBundleUrl() != "" {
				endpoints = append(endpoints, pemCert.GetSpiffeBundleUrl())
			}
		}

		err = tb.UpdateTrustAnchor(&TrustAnchorUpdate{
			TrustAnchorConfig: TrustAnchorConfig{Certs: certs},
			Source:            SourceMeshConfig,
		})
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
	remoteCerts := []string{}

	currentTrustDomain := spiffe.GetTrustDomain()
	for _, endpoint := range remoteEndpoints {
		trustDomainAnchorMap, err := spiffe.RetrieveSpiffeBundleRootCerts(
			map[string]string{currentTrustDomain: endpoint}, tb.remoteCaCertPool, remoteTimeout)
		if err != nil {
			trustBundleLog.Errorf("unable to fetch trust Anchors from endpoint %s: %s", endpoint, err)
			continue
		}
		certs := trustDomainAnchorMap[currentTrustDomain]
		for _, cert := range certs {
			certStr := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
			trustBundleLog.Debugf("from endpoint %v, fetched trust anchor cert: %v", endpoint, certStr)
			remoteCerts = append(remoteCerts, certStr)
		}
	}
	err = tb.UpdateTrustAnchor(&TrustAnchorUpdate{
		TrustAnchorConfig: TrustAnchorConfig{Certs: remoteCerts},
		Source:            sourceSpiffeEndpoints,
	})
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
