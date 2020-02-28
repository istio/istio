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

package util

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

type JWTPolicy string

const (
	FirstPartyJWT JWTPolicy = "first-party-jwt"
	ThirdPartyJWT JWTPolicy = "third-party-jwt"
)

// DetectSupportedJWTPolicy queries the api-server to detect whether it has TokenRequest support
func DetectSupportedJWTPolicy(config *rest.Config) (JWTPolicy, error) {
	if config == nil {
		// this happens in unit tests- there's no such thing as a fake config
		// TODO(dgn): refactor to use Client instead of Config, so this can be faked
		return ThirdPartyJWT, nil
	}

	d, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return "", err
	}
	_, s, err := d.ServerGroupsAndResources()
	// This may fail if any api service is down. We should only fail if the specific API we care about failed
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) {
			derr := err.(*discovery.ErrGroupDiscoveryFailed)
			if _, f := derr.Groups[schema.GroupVersion{Group: "authentication.k8s.io", Version: "v1"}]; f {
				return "", err
			}
		} else {
			return "", err
		}
	}
	for _, res := range s {
		for _, api := range res.APIResources {
			// Appearance of this API indicates we do support third party jwt token
			if api.Name == "serviceaccounts/token" {
				return ThirdPartyJWT, nil
			}
		}
	}
	return FirstPartyJWT, nil
}
