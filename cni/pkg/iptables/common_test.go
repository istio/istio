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

package iptables

func GetCommonInPodTestCases() []struct {
	name         string
	config       func(cfg *IptablesConfig)
	podOverrides PodLevelOverrides
} {
	return []struct {
		name         string
		config       func(cfg *IptablesConfig)
		podOverrides PodLevelOverrides
	}{
		{
			name: "default",
			config: func(cfg *IptablesConfig) {
				cfg.RedirectDNS = true
			},
			podOverrides: PodLevelOverrides{},
		},
		{
			name: "ingress",
			config: func(cfg *IptablesConfig) {
			},
			podOverrides: PodLevelOverrides{IngressMode: true},
		},
		{
			name: "virtual_interfaces",
			config: func(cfg *IptablesConfig) {
			},
			podOverrides: PodLevelOverrides{
				VirtualInterfaces: []string{"fake1s0f0", "fake1s0f1"},
			},
		},
		{
			name: "ingress_and_virtual_interfaces",
			config: func(cfg *IptablesConfig) {
			},
			podOverrides: PodLevelOverrides{
				IngressMode:       true,
				VirtualInterfaces: []string{"fake1s0f0", "fake1s0f1"},
			},
		},
		{
			name: "dns_pod_enabled_and_off_globally",
			config: func(cfg *IptablesConfig) {
				cfg.RedirectDNS = false
			},
			podOverrides: PodLevelOverrides{DNSProxy: PodDNSEnabled},
		},
		{
			name: "dns_pod_disabled_and_on_globally",
			config: func(cfg *IptablesConfig) {
				cfg.RedirectDNS = true
			},
			podOverrides: PodLevelOverrides{DNSProxy: PodDNSDisabled},
		},
	}
}

func GetCommonHostTestCases() []struct {
	name   string
	config func(cfg *IptablesConfig)
} {
	return []struct {
		name   string
		config func(cfg *IptablesConfig)
	}{
		{
			name: "hostprobe",
			config: func(cfg *IptablesConfig) {
				cfg.RedirectDNS = true
			},
		},
	}
}
