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
	name        string
	config      func(cfg *Config)
	ingressMode bool
} {
	return []struct {
		name        string
		config      func(cfg *Config)
		ingressMode bool
	}{
		{
			name: "default",
			config: func(cfg *Config) {
				cfg.RedirectDNS = true
			},
			ingressMode: false,
		},
		{
			name: "ingress",
			config: func(cfg *Config) {
			},
			ingressMode: true,
		},
	}
}

func GetCommonHostTestCases() []struct {
	name   string
	config func(cfg *Config)
} {
	return []struct {
		name   string
		config func(cfg *Config)
	}{
		{
			name: "hostprobe",
			config: func(cfg *Config) {
				cfg.RedirectDNS = true
			},
		},
	}
}
