// Copyright 2018 Istio Authors
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

package platform

import (
	"cloud.google.com/go/compute/metadata"

	"istio.io/istio/pkg/log"
)

// GetZone returns the platform-specific zone. Currently only GCP is supported.
func GetZone() string {
	if metadata.OnGCE() {
		z, err := metadata.Zone()
		if err != nil {
			log.Warnf("Unable to get the GCP zone: %v", err)
			return ""
		}
		return z
	}

	return ""
}
