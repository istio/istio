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
package bootstrap

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

var trustBundleLog = log.RegisterScope("trustBundle", "Workload mTLS trust bundle logs", 0)

// source is all possible sources of MeshConfig
type Source int

type TrustAnchorConfig struct {
	source Source
	certs  []string
}

type TrustAnchorUpdate struct {
	TrustAnchorConfig
}

type TrustBundle struct {
	sourceConfig     map[Source]*TrustAnchorConfig
	mutex            sync.RWMutex
	mergedCerts      []string
	anchorUpdateChan chan *TrustAnchorUpdate
}

const (
	SourceIstioCA Source = iota
	SourceMeshConfig
	SourceIstioRA
	// SOURCE_SPIFFE_ENDPOINT
	UpdateChannelLen = 10
)

func NewTrustBundle() *TrustBundle {
	tb := &TrustBundle{
		sourceConfig: map[Source]*TrustAnchorConfig{
			SourceIstioCA:    {source: SourceIstioCA, certs: []string{}},
			SourceMeshConfig: {source: SourceMeshConfig, certs: []string{}},
			SourceIstioRA:    {source: SourceIstioRA, certs: []string{}},
			// SOURCE_SPIFFE_ENDPOINT:        &TrustAnchorConfig{source: SOURCE_SPIFFE_ENDPOINT, certs: []string{}},
		},
		mergedCerts:      []string{},
		anchorUpdateChan: make(chan *TrustAnchorUpdate, UpdateChannelLen),
	}
	return tb
}

// FetchTrustBundle : Retrieves all the trustAnchors for current Spiffee Trust Domain
func (tb *TrustBundle) FetchTrustBundle() []string {
	tb.mutex.RLock()
	defer tb.mutex.RUnlock()
	trustedCerts := make([]string, len(tb.mergedCerts))
	copy(trustedCerts, tb.mergedCerts)
	return trustedCerts
}

func (tb *TrustBundle) verifyTrustAnchor(trustAnchor string) error {
	// S,G: TODO Validate that these are CA certs. Any other checks?
	return nil
}

// Should be done as part of a go thread
func (tb *TrustBundle) mergeInternal() {
	var ok bool
	mergeCerts := []string{}
	certMap := make(map[string]bool)

	for _, configSource := range tb.sourceConfig {
		for _, cert := range configSource.certs {
			if _, ok = certMap[cert]; !ok {
				certMap[cert] = true
				mergeCerts = append(mergeCerts, cert)
			}
		}
	}
	tb.mutex.Lock()
	tb.mergedCerts = mergeCerts
	tb.mutex.Unlock()
}

// External Function to merge a TrustAnchor config with the existing TrustBundle
// Should only be one writer
func (tb *TrustBundle) MergeTrustBundle(anchorConfig *TrustAnchorUpdate) error {
	var ok bool
	var err error

	cachedConfig, ok := tb.sourceConfig[anchorConfig.source]
	if !ok {
		return fmt.Errorf("invalid source of TrustBundle configuration %v", anchorConfig.source)
	}

	// Check if anything needs to be changed at all
	if reflect.DeepEqual(anchorConfig.certs, cachedConfig.certs) {
		trustBundleLog.Infof("no change to trustAnchor configuration after recent update")
		return nil
	}

	for _, cert := range anchorConfig.certs {
		err = tb.verifyTrustAnchor(cert)
		if err != nil {
			return err
		}
	}
	tb.sourceConfig[anchorConfig.TrustAnchorConfig.source] = &anchorConfig.TrustAnchorConfig
	tb.mergeInternal()

	log.Debugf("updating Source %v with certs %v",
		anchorConfig.TrustAnchorConfig.source,
		strings.Join(anchorConfig.TrustAnchorConfig.certs, "\n\n"))

	// S,G: Make the call to notify XDS of the changes and request a push configuration
	return nil
}

// EnqueueAnchorUpdate: Enqueue a Anchor Configuration Update to the TrustBundle
func (tb *TrustBundle) EnqueueAnchorUpdate(cfg *TrustAnchorUpdate) {
	trustBundleLog.Debugf("enqueued trustAnchor Update %v", cfg)
	tb.anchorUpdateChan <- cfg
}

// processUpdates: R
func (tb *TrustBundle) processUpdates(stop <-chan struct{}) {
	// TODO: Log
	select {
	case <-stop:
		return
	case anchorConfig := <-tb.anchorUpdateChan:
		trustBundleLog.Debugf("dequeued trustAnchor Update %v", anchorConfig)
		err := tb.MergeTrustBundle(anchorConfig)
		if err != nil {
			trustBundleLog.Errorf("unable to apply trustAnchor update. Encountered error %v", err)
		}
	}
}

// AddPeriodicMeshConfigUpdate
func (tb *TrustBundle) AddMeshConfigUpdate(cfg *meshconfig.MeshConfig) {
	if cfg != nil {
		certs := []string{}
		for _, pemCert := range cfg.GetCaCertificates() {
			certs = append(certs, pemCert.GetPem())
		}
		tb.EnqueueAnchorUpdate(&TrustAnchorUpdate{TrustAnchorConfig{
			source: SourceMeshConfig,
			certs:  certs,
		}})
	}
}

// AddPeriodicIstioCAConfigUpdate
func (tb *TrustBundle) AddPeriodicIstioCARootUpdate(stop <-chan struct{}, certBundle util.KeyCertBundle, periodicity time.Duration) {
	select {
	case <-time.After(periodicity):
		rootCerts := []string{string(certBundle.GetRootCertPem())}
		tb.EnqueueAnchorUpdate(&TrustAnchorUpdate{TrustAnchorConfig: TrustAnchorConfig{
			source: SourceIstioCA,
			certs:  rootCerts,
		}})
	case <-stop:
		return
	}
}
