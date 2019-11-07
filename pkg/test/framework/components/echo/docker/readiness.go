// Copyright 2019 Istio Authors
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

package docker

import (
	"fmt"
	"net/http"
)

type probeFunc func() error

func noSidecarReadinessProbe(hostHTTPPort uint16) probeFunc {
	return func() error {
		probeURL := fmt.Sprintf("http://%s:%d", localhost, hostHTTPPort)
		return doProbeHTTP(probeURL, "echo server")
	}
}

func sidecarReadinessProbe(hostAgentStatusPort uint16) probeFunc {
	return func() error {
		probeURL := fmt.Sprintf("http://%s:%d/healthz/ready", localhost, hostAgentStatusPort)
		return doProbeHTTP(probeURL, "pilot agent")
	}
}

func doProbeHTTP(probeURL, serviceName string) error {
	resp, err := http.Get(probeURL)
	if err != nil {
		return fmt.Errorf("readiness probe failed for %s: %v", serviceName, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s not ready. HTTP status: %d", serviceName, resp.StatusCode)
	}

	return nil
}
