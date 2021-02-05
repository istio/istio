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
	"sync"
)

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
	SourcePluginCertConfigMap Source = iota
	SourceMeshConfig
	SourceSelfSigned
	// SOURCE_SPIFFE_ENDPOINT
)

func NewTrustBundle() *TrustBundle {
	tb := &TrustBundle{
		sourceConfig: map[Source]*TrustAnchorConfig{
			SourcePluginCertConfigMap: &TrustAnchorConfig{source: SourcePluginCertConfigMap, certs: []string{}},
			SourceMeshConfig:          &TrustAnchorConfig{source: SourceMeshConfig, certs: []string{}},
			SourceSelfSigned:          &TrustAnchorConfig{source: SourceSelfSigned, certs: []string{}},
			// SOURCE_SPIFFE_ENDPOINT:        &TrustAnchorConfig{source: SOURCE_SPIFFE_ENDPOINT, certs: []string{}},
		},
		mergedCerts:      []string{},
		anchorUpdateChan: make(chan *TrustAnchorUpdate),
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

func (tb *TrustBundle) verifyTrustAnchor(trustAnchor string) bool {
	// S,G: TODO Validate that these are CA certs. Any other checks?
	return true
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
	defer tb.mutex.Unlock()
}

// External Function to merge a TrustAnchor config with the existing TrustBundle
// Should only be one writer
func (tb *TrustBundle) MergeTrustBundle(anchorConfig *TrustAnchorUpdate) error {
	var ok bool

	cachedConfig, ok := tb.sourceConfig[anchorConfig.source]
	if !ok {
		return fmt.Errorf("invalid source of TrustBundle configuration %v", anchorConfig.source)
	}

	// Check if anything needs to be changed at all
	if reflect.DeepEqual(anchorConfig.certs, cachedConfig.certs) {
		return nil
	}

	verifiedAnchorConfig := &TrustAnchorConfig{source: anchorConfig.source, certs: []string{}}
	for _, cert := range anchorConfig.certs {
		if !tb.verifyTrustAnchor(cert) {
			// S,G Log Error Identifying PEM certificate and source
			continue
		}
		verifiedAnchorConfig.certs = append(verifiedAnchorConfig.certs, cert)
	}

	tb.sourceConfig[verifiedAnchorConfig.source] = verifiedAnchorConfig

	tb.mergeInternal()

	// S,G: Make the call to notify XDS of the changes and request a push configuration
	return nil
}

// EnqueueAnchorUpdate: Enqueue a Anchor Configuration Update to the TrustBundle
func (tb *TrustBundle) EnqueueAnchorUpdate(cfg *TrustAnchorUpdate) {
	// TODO: Log Info
	tb.anchorUpdateChan <- cfg
}

// processUpdates: R
func (tb *TrustBundle) processUpdates(stop <-chan struct{}) {
	// TODO: Log
	select {
	case <-stop:
		return
	case anchorConfig := <-tb.anchorUpdateChan:
		// TODO: Log Debug
		_ = tb.MergeTrustBundle(anchorConfig)
		// TODO: Log Error messages
	}
}
