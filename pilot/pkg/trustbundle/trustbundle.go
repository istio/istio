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

	meshconfig "istio.io/api/mesh/v1alpha1"
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

type TrustBundle struct {
	sourceConfig map[Source]TrustAnchorConfig
	mutex        sync.RWMutex
	mergedCerts  []string
	updatecb     func()
}

var trustBundleLog = log.RegisterScope("trustBundle", "Workload mTLS trust bundle logs", 0)

const (
	SourceIstioCA Source = iota
	SourceMeshConfig
	SourceIstioRA
)

func checkSameCerts(certs1 []string, certs2 []string) bool {
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
func NewTrustBundle() *TrustBundle {
	tb := &TrustBundle{
		sourceConfig: map[Source]TrustAnchorConfig{
			SourceIstioCA:    {Certs: []string{}},
			SourceMeshConfig: {Certs: []string{}},
			SourceIstioRA:    {Certs: []string{}},
		},
		mergedCerts: []string{},
		updatecb:    nil,
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

	tb.mutex.Lock()
	defer tb.mutex.Unlock()

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
func (tb *TrustBundle) UpdateTrustAnchor(anchorConfig *TrustAnchorUpdate) error {
	var ok bool
	var err error

	cachedConfig, ok := tb.sourceConfig[anchorConfig.Source]
	if !ok {
		return fmt.Errorf("invalid source of TrustBundle configuration %v", anchorConfig.Source)
	}

	// Check if anything needs to be changed at all
	if checkSameCerts(anchorConfig.Certs, cachedConfig.Certs) {
		trustBundleLog.Debugf("no change to trustAnchor configuration after recent update")
		return nil
	}

	for _, cert := range anchorConfig.Certs {
		err = verifyTrustAnchor(cert)
		if err != nil {
			return err
		}
	}
	tb.sourceConfig[anchorConfig.Source] = anchorConfig.TrustAnchorConfig
	tb.mergeInternal()

	trustBundleLog.Infof("updating Source %v with certs %v",
		anchorConfig.Source,
		strings.Join(anchorConfig.TrustAnchorConfig.Certs, "\n"))

	if tb.updatecb != nil {
		tb.updatecb()
	}
	return nil
}

// AddMeshConfigUpdate : Update trustAnchor configurations from meshConfig
func (tb *TrustBundle) AddMeshConfigUpdate(cfg *meshconfig.MeshConfig) {
	if cfg != nil {
		certs := []string{}
		for _, pemCert := range cfg.GetCaCertificates() {
			certs = append(certs, pemCert.GetPem())
		}
		err := tb.UpdateTrustAnchor(&TrustAnchorUpdate{
			TrustAnchorConfig: TrustAnchorConfig{Certs: certs},
			Source:            SourceMeshConfig,
		})
		if err != nil {
			trustBundleLog.Errorf("failed to update meshConfig trustAnchors: %v", err)
		}
	}
}
