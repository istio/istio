// Copyright 2017 Istio Authors
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

package envoy

import (
	"istio.io/istio/pkg/spiffe"
)

const (
	// Service accounts for Mixer and Pilot, these are hardcoded values at setup time
	mixerSvcAccName string = "istio-mixer-service-account"
	pilotSvcAccName string = "istio-pilot-service-account"
)

// GetMixerSAN returns the SAN used for mixer mTLS
func GetMixerSAN(ns string) string {
	return spiffe.MustGenSpiffeURI(ns, mixerSvcAccName)
}

// GetPilotSAN returns the SAN used for pilot mTLS
func GetPilotSAN(ns string) string {
	return spiffe.MustGenSpiffeURI(ns, pilotSvcAccName)
}
