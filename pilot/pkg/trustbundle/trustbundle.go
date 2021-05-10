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

	"go.uber.org/atomic"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/spiffe"
	"istio.io/pkg/log"
)

// source is all possible sources of MeshConfig
type Source int

type TrustAnchorConfig struct {
	Certs []string
}

type TrustAnchorUpdate struct {
	TrustAnchorConfig
	Source Source
}

type trustAnchorEndpoint struct {
	CachedTrustAnchors TrustAnchorConfig
	RetryCount         int
}

type TrustBundle struct {
	sourceConfig       map[Source]TrustAnchorConfig
	mutex              sync.RWMutex
	mergedCerts        []string
	updatecb           func()
	endpointCfg        []string
	endpointCfgMutex   sync.RWMutex
	endpoints          map[string]*trustAnchorEndpoint
	endpointUpdateChan chan struct{}
	remoteCaCertPool   *x509.CertPool
	createTime         time.Time
	syncTimeout        time.Duration
	initDone           atomic.Bool
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

// NewTrustBundle: Returns a new trustbundle
func NewTrustBundle(remoteCaCertPool *x509.CertPool, syncTimeout time.Duration) *TrustBundle {
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
		endpoints:          map[string]*trustAnchorEndpoint{},
		createTime:         time.Now(),
		syncTimeout:        syncTimeout,
	}
	tb.initDone.Store(false)
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
	var ok bool
	mergeCerts := []string{}
	certMap := make(map[string]struct{})

	for _, configSource := range tb.sourceConfig {
		for _, cert := range configSource.Certs {
			if _, ok = certMap[cert]; !ok {
				certMap[cert] = struct{}{}
				mergeCerts = append(mergeCerts, cert)
			}
		}
	}
	tb.mergedCerts = mergeCerts
	sort.Strings(tb.mergedCerts)
}

// UpdateTrustAnchor: External Function to merge a TrustAnchor config with the existing TrustBundle
func (tb *TrustBundle) updateTrustAnchorInternal(anchorConfig *TrustAnchorUpdate) (bool, error) {
	var ok bool
	var err error

	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	cachedConfig, ok := tb.sourceConfig[anchorConfig.Source]

	if !ok {
		return false, fmt.Errorf("invalid source of TrustBundle configuration %v", anchorConfig.Source)
	}

	// Check if anything needs to be changed at all
	if isEqSliceStr(anchorConfig.Certs, cachedConfig.Certs) {
		trustBundleLog.Debugf("no change to trustAnchor configuration after recent update")
		return false, nil
	}

	for _, cert := range anchorConfig.Certs {
		err = verifyTrustAnchor(cert)
		if err != nil {
			return false, err
		}
	}

	tb.sourceConfig[anchorConfig.Source] = anchorConfig.TrustAnchorConfig
	tb.mergeInternal()

	trustBundleLog.Infof("updating Source %v with certs %v",
		anchorConfig.Source,
		strings.Join(anchorConfig.TrustAnchorConfig.Certs, "\n"))

	return true, nil
}

func (tb *TrustBundle) UpdateTrustAnchor(anchorConfig *TrustAnchorUpdate) error {
	updated, err := tb.updateTrustAnchorInternal(anchorConfig)
	if updated && err == nil && tb.updatecb != nil {
		tb.updatecb()
	}
	return err
}

func (tb *TrustBundle) updateCfgRemoteEndpoint(spiffeEndpoints []string) {
	var changed bool = false
	var endpointURI string

	newEndpoints := make(map[string]struct{}, len(spiffeEndpoints))
	for _, endpoint := range spiffeEndpoints {
		newEndpoints[endpoint] = struct{}{}
	}

	for endpointURI = range newEndpoints {
		if _, ok := tb.endpoints[endpointURI]; !ok {
			changed = true
			tb.endpoints[endpointURI] = &trustAnchorEndpoint{
				CachedTrustAnchors: TrustAnchorConfig{Certs: []string{}}, RetryCount: 0,
			}
		}
	}

	for endpointURI = range tb.endpoints {
		if _, ok := newEndpoints[endpointURI]; !ok {
			changed = true
			delete(tb.endpoints, endpointURI)
		}
	}
	if changed {
		trustBundleLog.Infof("updated remote endpoints  : %v", spiffeEndpoints)
	}
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
		tb.endpointCfgMutex.Lock()
		tb.endpointCfg = endpoints
		tb.endpointCfgMutex.Unlock()
		tb.endpointUpdateChan <- struct{}{}
	}
	return nil
}

func (tb *TrustBundle) updateRemoteTrustAnchors() {
	var err error
	var remoteCerts []string
	var fetchFail bool = false

	tb.endpointCfgMutex.RLock()
	endpointCfg := make([]string, len(tb.endpointCfg))
	copy(endpointCfg, tb.endpointCfg)
	tb.endpointCfgMutex.RUnlock()

	currentTrustDomain := spiffe.GetTrustDomain()
	tb.updateCfgRemoteEndpoint(endpointCfg)
	remoteEndpoints := tb.endpoints

	for endpointURI, remoteTrustAnchor := range remoteEndpoints {
		trustDomainAnchorMap, err := spiffe.RetrieveSpiffeBundleRootCerts(
			map[string]string{currentTrustDomain: endpointURI}, tb.remoteCaCertPool, remoteTimeout)
		if err == nil {
			remoteTrustAnchor.Reset()
			certs := trustDomainAnchorMap[currentTrustDomain]
			for _, cert := range certs {
				certStr := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
				trustBundleLog.Debugf("from endpoint %v, fetched trust anchor cert: %v", endpointURI, certStr)
				remoteCerts = append(remoteCerts, certStr)
				remoteTrustAnchor.AddCert(certStr)
			}
		} else {
			fetchFail = true
			remoteTrustAnchor.IncrementRetry()
			trustBundleLog.Infof("unable to fetch trustAnchor from endpoint %s after %v retries : %v",
				endpointURI, remoteTrustAnchor.RetryCount, err)
			// Re-use older trustAnchors associated with this endpoint
			anchors := remoteTrustAnchor.CachedTrustAnchors.Certs
			remoteCerts = append(remoteCerts, anchors...)
		}
	}
	if !fetchFail {
		tb.initDone.Store(true)
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
			tb.updateRemoteTrustAnchors()
		case <-stop:
			trustBundleLog.Infof("stop processing endpoint trustAnchor pdates")
			return
		case <-tb.endpointUpdateChan:
			tb.updateRemoteTrustAnchors()
			trustBundleLog.Infof("processing endpoint trustAnchor Updates for config change")
		}
	}
}

func (tb *TrustBundle) HasSynced() bool {
	initDone := tb.initDone.Load()
	if !initDone && time.Since(tb.createTime) < tb.syncTimeout {
		return false
	}
	if !initDone {
		trustBundleLog.Infof("pending trustAnchor Sync from some remote endpoints")
	}
	return true
}

func (r *trustAnchorEndpoint) IncrementRetry() {
	r.RetryCount++
}

func (r *trustAnchorEndpoint) AddCert(cert string) {
	r.CachedTrustAnchors.Certs = append(r.CachedTrustAnchors.Certs, cert)
}

func (r *trustAnchorEndpoint) Reset() {
	r.CachedTrustAnchors.Certs = []string{}
	r.RetryCount = 0
}
