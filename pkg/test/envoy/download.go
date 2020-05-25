//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package envoy

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"istio.io/istio/pkg/test/deps"
)

var (
	LatestStableSHA string
	LinuxReleaseURL string
	AuthHeader      *http.Header
)

func init() {
	envoyBaseURL := "https://storage.googleapis.com/istio-build/proxy"
	if override, f := os.LookupEnv("ISTIO_ENVOY_BASE_URL"); f {
		envoyBaseURL = override
	}

	if authHeader, f := os.LookupEnv("AUTH_HEADER"); f {
		kv := strings.Split(authHeader, ": ")
		AuthHeader = &http.Header{kv[0]: kv[1:]}
	}

	for _, dep := range deps.Istio {
		if dep.Name == "PROXY_REPO_SHA" {
			LatestStableSHA = dep.LastStableSHA
			LinuxReleaseURL = fmt.Sprintf("%s/envoy-alpha-%s.tar.gz", envoyBaseURL, LatestStableSHA)
			return
		}
	}

	panic(fmt.Errorf("envoy SHA not found in: \n%v", deps.Istio))
}
