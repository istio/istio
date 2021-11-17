//go:build !linux
// +build !linux

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
package capture

import (
	"errors"

	"istio.io/istio/tools/istio-iptables/pkg/builder"
	"istio.io/istio/tools/istio-iptables/pkg/config"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

var (
	// ErrNotImplemented is returned when a requested feature is not implemented.
	ErrNotImplemented = errors.New("not implemented")
)

func configureTProxyRoutes(cfg *config.Config) error {
	return ErrNotImplemented
}

func NewIptablesConfigurator(cfg *config.Config, ext dep.Dependencies) *IptablesConfigurator {
	return nil
}

func SplitV4V6(ips []string) (ipv4 []string, ipv6 []string) {
	return
}

func ConfigureRoutes(cfg *config.Config, ext dep.Dependencies) error {
	return ErrNotImplemented
}

func HandleDNSUDP(
	ops Ops, iptables *builder.IptablesBuilder, ext dep.Dependencies,
	cmd, proxyUID, proxyGID string, dnsServersV4 []string, dnsServersV6 []string, captureAllDNS bool) {
}

func (cfg *IptablesConfigurator) Run() {}

func (cfg *IptablesConfigurator) separateV4V6(cidrList string) (NetworkRange, NetworkRange, error) {
	return NetworkRange{}, NetworkRange{}, ErrNotImplemented
}
