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
	"reflect"
	"strings"
	"sync"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/log"
)

// source is all possible sources of MeshConfig
type Source int

type TrustAnchorConfig struct {
	Source Source
	Certs  []string
}

type TrustAnchorUpdate struct {
	TrustAnchorConfig
}

type TrustBundle struct {
	sourceConfig map[Source]TrustAnchorConfig
	mutex        sync.RWMutex
	mergedCerts  []string
	updatecb     func()
}

var (
	trustBundleLog = log.RegisterScope("trustBundle", "Workload mTLS trust bundle logs", 0)

	// GlobalWorkloadTrustbundle Needed to interface with XDS
	GlobalWorkloadTrustbundle *TrustBundle = nil
)

const (
	SourceIstioCA Source = iota
	SourceMeshConfig
	SourceIstioRA
	// SOURCE_SPIFFE_ENDPOINT
	UpdateChannelLen = 10
)

func NewTrustBundle(updatecb func()) *TrustBundle {
	tb := &TrustBundle{
		sourceConfig: map[Source]TrustAnchorConfig{
			SourceIstioCA:    {Source: SourceIstioCA, Certs: []string{}},
			SourceMeshConfig: {Source: SourceMeshConfig, Certs: []string{}},
			SourceIstioRA:    {Source: SourceIstioRA, Certs: []string{}},
			// SOURCE_SPIFFE_ENDPOINT:        &TrustAnchorConfig{source: SOURCE_SPIFFE_ENDPOINT, certs: []string{}},
		},
		mergedCerts: []string{},
		updatecb:    updatecb,
	}

	GlobalWorkloadTrustbundle = tb
	return tb
}

// GetTrustBundle : Retrieves all the trustAnchors for current Spiffee Trust Domain
func (tb *TrustBundle) GetTrustBundle() []string {
	tb.mutex.RLock()
	defer tb.mutex.RUnlock()
	trustedCerts := make([]string, len(tb.mergedCerts))
	copy(trustedCerts, tb.mergedCerts)
	return trustedCerts
}

func (tb *TrustBundle) verifyTrustAnchor(trustAnchor string) error {
	block, _ := pem.Decode([]byte(trustAnchor))
	if block == nil {
		return fmt.Errorf("failed to decode pem certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse X.509 certificate")
	}
	if !cert.IsCA {
		return fmt.Errorf("certificate is not a CA certificate")
	}
	return nil
}

func (tb *TrustBundle) mergeInternal() {
	var ok bool
	mergeCerts := []string{}
	certMap := make(map[string]bool)

	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	for _, configSource := range tb.sourceConfig {
		for _, cert := range configSource.Certs {
			if _, ok = certMap[cert]; !ok {
				certMap[cert] = true
				mergeCerts = append(mergeCerts, cert)
			}
		}
	}
	tb.mergedCerts = mergeCerts
}

// External Function to merge a TrustAnchor config with the existing TrustBundle
// Should only be one writer
func (tb *TrustBundle) UpdateTrustAnchor(anchorConfig *TrustAnchorUpdate) error {
	var ok bool
	var err error

	cachedConfig, ok := tb.sourceConfig[anchorConfig.Source]
	if !ok {
		return fmt.Errorf("invalid source of TrustBundle configuration %v", anchorConfig.Source)
	}

	// Check if anything needs to be changed at all
	if reflect.DeepEqual(anchorConfig.Certs, cachedConfig.Certs) {
		trustBundleLog.Infof("no change to trustAnchor configuration after recent update")
		return nil
	}

	for _, cert := range anchorConfig.Certs {
		err = tb.verifyTrustAnchor(cert)
		if err != nil {
			return err
		}
	}
	tb.sourceConfig[anchorConfig.TrustAnchorConfig.Source] = anchorConfig.TrustAnchorConfig
	tb.mergeInternal()

	trustBundleLog.Debugf("updating Source %v with certs %v",
		anchorConfig.TrustAnchorConfig.Source,
		strings.Join(anchorConfig.TrustAnchorConfig.Certs, "\n\n"))

	tb.updatecb()
	return nil
}

// AddPeriodicMeshConfigUpdate
func (tb *TrustBundle) AddMeshConfigUpdate(cfg *meshconfig.MeshConfig) {
	if cfg != nil {
		certs := []string{}
		for _, pemCert := range cfg.GetCaCertificates() {
			certs = append(certs, pemCert.GetPem())
		}
		err := tb.UpdateTrustAnchor(&TrustAnchorUpdate{TrustAnchorConfig{
			Source: SourceMeshConfig,
			Certs:  certs,
		}})
		if err != nil {
			trustBundleLog.Errorf("failed to update meshConfig trustAnchors because %v")
		}
	}
}
