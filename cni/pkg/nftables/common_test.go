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

package nftables

import "istio.io/istio/cni/pkg/config"

func GetCommonInPodTestCases() []struct {
	name         string
	config       func(cfg *config.AmbientConfig)
	podOverrides config.PodLevelOverrides
} {
	return []struct {
		name         string
		config       func(cfg *config.AmbientConfig)
		podOverrides config.PodLevelOverrides
	}{
		{
			name: "default",
			config: func(cfg *config.AmbientConfig) {
				cfg.RedirectDNS = true
			},
			podOverrides: config.PodLevelOverrides{},
		},
		{
			name: "ingress",
			config: func(cfg *config.AmbientConfig) {
			},
			podOverrides: config.PodLevelOverrides{IngressMode: true},
		},
		{
			name: "virtual_interfaces",
			config: func(cfg *config.AmbientConfig) {
			},
			podOverrides: config.PodLevelOverrides{
				VirtualInterfaces: []string{"fake1s0f0", "fake1s0f1"},
			},
		},
		{
			name: "ingress_and_virtual_interfaces",
			config: func(cfg *config.AmbientConfig) {
			},
			podOverrides: config.PodLevelOverrides{
				IngressMode:       true,
				VirtualInterfaces: []string{"fake1s0f0", "fake1s0f1"},
			},
		},
		{
			name: "dns_pod_enabled_and_off_globally",
			config: func(cfg *config.AmbientConfig) {
				cfg.RedirectDNS = false
			},
			podOverrides: config.PodLevelOverrides{DNSProxy: config.PodDNSEnabled},
		},
		{
			name: "dns_pod_disabled_and_on_globally",
			config: func(cfg *config.AmbientConfig) {
				cfg.RedirectDNS = true
			},
			podOverrides: config.PodLevelOverrides{DNSProxy: config.PodDNSDisabled},
		},
	}
}

func GetCommonHostTestCases() []struct {
	name   string
	config func(cfg *config.AmbientConfig)
} {
	return []struct {
		name   string
		config func(cfg *config.AmbientConfig)
	}{
		{
			name: "hostprobe",
			config: func(cfg *config.AmbientConfig) {
				cfg.RedirectDNS = true
			},
		},
	}
}
