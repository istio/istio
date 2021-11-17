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
	"net"

	"istio.io/istio/tools/istio-iptables/pkg/builder"
	"istio.io/istio/tools/istio-iptables/pkg/config"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

type Ops int

const (
	// AppendOps performs append operations of rules
	AppendOps Ops = iota
	// DeleteOps performs delete operations of rules
	DeleteOps

	// In TPROXY mode, mark the packet from envoy outbound to app by podIP,
	// this is to prevent it being intercepted to envoy inbound listener.
	outboundMark = "1338"
)

type IptablesConfigurator struct {
	iptables *builder.IptablesBuilder
	// TODO(abhide): Fix dep.Dependencies with better interface
	ext dep.Dependencies
	cfg *config.Config
}

type NetworkRange struct {
	IsWildcard    bool
	IPNets        []*net.IPNet
	HasLoopBackIP bool
}
