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

package kube

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework/components/echo"
)

type podSelector struct {
	labels map[string]string
}

func (s podSelector) String() string {
	str := ""
	i := 0
	for l, v := range s.labels {
		str += l + "=" + v
		i++
		if i != len(s.labels) {
			str += ","
		}
	}
	return str
}

func newPodSelector(cfg echo.Config) podSelector {
	appLabel := "app"
	if cfg.DeployAsVM {
		appLabel = constants.TestVMLabel
	}
	return podSelector{
		labels: map[string]string{
			appLabel:  cfg.Service,
			"version": cfg.Version,
		},
	}
}

func serviceAccount(cfg echo.Config) string {
	if cfg.ServiceAccount {
		return cfg.Service
	}
	if cfg.DeployAsVM {
		return "default"
	}
	return ""
}

// workloadHasSidecar returns true if the input endpoint is deployed with sidecar injected based on the config.
func workloadHasSidecar(cfg echo.Config, podName string) bool {
	// Match workload first.
	for _, w := range cfg.Subsets {
		if strings.HasPrefix(podName, fmt.Sprintf("%v-%v", cfg.Service, w.Version)) {
			return w.Annotations.GetBool(echo.SidecarInject)
		}
	}
	return true
}
