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

package app

import (
	"crypto/tls"
	"errors"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/util/sets"
)

// insecureTLSCipherNames returns a list of insecure cipher suite names implemented by crypto/tls
// which have security issues.
func insecureTLSCipherNames() []string {
	cipherKeys := sets.New[string]()
	for _, cipher := range tls.InsecureCipherSuites() {
		cipherKeys.Insert(cipher.Name)
	}
	return sets.SortedList(cipherKeys)
}

// secureTLSCipherNames returns a list of secure cipher suite names implemented by crypto/tls.
func secureTLSCipherNames() []string {
	cipherKeys := sets.New[string]()
	for _, cipher := range tls.CipherSuites() {
		cipherKeys.Insert(cipher.Name)
	}
	return sets.SortedList(cipherKeys)
}

// validateClusterAliases validates that there's no more then alias per clusterID.
func validateClusterAliases(clusterAliases map[string]string) error {
	seenClusterIDs := sets.New[string]()
	for _, clusterID := range clusterAliases {
		if seenClusterIDs.InsertContains(clusterID) {
			return errors.New("More than one cluster alias for cluster id: " + clusterID)
		}
	}
	return nil
}

func validateFlags(serverArgs *bootstrap.PilotArgs) error {
	if serverArgs == nil {
		return nil
	}

	// If keepaliveMaxServerConnectionAge is negative, istiod crash
	// https://github.com/istio/istio/issues/27257
	if err := validation.ValidateMaxServerConnectionAge(serverArgs.KeepaliveOptions.MaxServerConnectionAge); err != nil {
		return err
	}

	if err := validateClusterAliases(serverArgs.RegistryOptions.KubeOptions.ClusterAliases); err != nil {
		return err
	}

	_, err := bootstrap.TLSCipherSuites(serverArgs.ServerOptions.TLSOptions.TLSCipherSuites)

	// TODO: add validation for other flags
	return err
}
