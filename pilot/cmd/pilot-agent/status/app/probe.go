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

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// ProbeMapEnvName is the name of the command line flag for pilot agent to pass app prober config.
	// The json encoded string to pass app HTTP probe information from injector(istioctl or webhook).
	// For example, --ISTIO_KUBE_APP_PROBERS='{"/app-health/httpbin/livez":{"path": "/hello", "port": 8080}.
	// indicates that httpbin container liveness prober port is 8080 and probing path is /hello.
	// This environment variable should never be set manually.
	ProbeMapEnvName = "ISTIO_KUBE_APP_PROBERS"
)

var (
	appProberPatternStr = `^/app-health/[^/]+/(livez|readyz)$`
	appProberPattern    = regexp.MustCompile(appProberPatternStr)
)

// ProbeURLPath the URL path for a pilot agent prober.
type ProbeURLPath string

// ProbeMap holds the information about a Kubernetes pod prober.
// It's a map from the pilot agent prober URL path to the Kubernetes Prober config.
// For example, "/app-health/hello-world/livez" entry contains livenss prober config for
// container "hello-world".
type ProbeMap map[ProbeURLPath]*corev1.HTTPGetAction

// ReadinessProbeURLPath returns the URL for probing app readiness for the given container.
func ReadinessProbeURLPath(container string) ProbeURLPath {
	return ProbeURLPath("/app-health/" + container + "/readyz")
}

// LivenessProbeURLPath returns the URL for probing app health for the given container.
func LivenessProbeURLPath(container string) ProbeURLPath {
	return ProbeURLPath("/app-health/" + container + "/livez")
}

// GetProbeMapEnv returns the value of the environment variable providing the app prober configuration.
// The value is a json encoded form of ProbeMap. For example,
// '{"/app-health/httpbin/livez":{"path": "/hello", "port": 8080}' indicates that the liveness prober port
// for the httpbin application is 8080 and probing path is /hello.
func GetKubeProberEnv() string {
	return os.Getenv(ProbeMapEnvName)
}

// ParseProbeMapJSON parses a JSON-encoded ProbeMap.
func ParseProbeMapJSON(probersJSON string) (ProbeMap, error) {
	if probersJSON == "" {
		return nil, nil
	}

	probeMap := make(ProbeMap)
	if err := json.Unmarshal([]byte(probersJSON), &probeMap); err != nil {
		return nil, fmt.Errorf("failed to decode app http prober err = %v, json string = %v", err, probersJSON)
	}

	// Validate the map key matching the regex pattern.
	for path, prober := range probeMap {
		if !appProberPattern.Match([]byte(path)) {
			return nil, fmt.Errorf(`invalid key, must be in form of regex pattern %s`, appProberPatternStr)
		}
		if prober.Port.Type != intstr.Int {
			return nil, fmt.Errorf("invalid prober config for %v, the port must be int type", path)
		}
	}
	return probeMap, nil
}
