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

package nodeagent

import (
	"net/netip"

	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/env"
)

var (
	PodNamespace                   = env.RegisterStringVar("POD_NAMESPACE", "", "pod's namespace").Get()
	SystemNamespace                = env.RegisterStringVar("SYSTEM_NAMESPACE", constants.IstioSystemNamespace, "istio system namespace").Get()
	PodName                        = env.RegisterStringVar("POD_NAME", "", "").Get()
	NodeName                       = env.RegisterStringVar("NODE_NAME", "", "").Get()
	Revision                       = env.RegisterStringVar("REVISION", "", "").Get()
	HostProbeSNATIP                = netip.MustParseAddr(env.RegisterStringVar("HOST_PROBE_SNAT_IP", DefaultHostProbeSNATIP, "").Get())
	HostProbeSNATIPV6              = netip.MustParseAddr(env.RegisterStringVar("HOST_PROBE_SNAT_IPV6", DefaultHostProbeSNATIPV6, "").Get())
	UseScopedIptablesLegacyLocking = env.RegisterBoolVar("AMBIENT_USE_SCOPED_XTABLES_LOCKING", true, "").Get()
	EnableAWSBranchENIProbe        = env.RegisterBoolVar("AMBIENT_ENABLE_AWS_BRANCH_ENI_PROBE", true,
		"If true, detect AWS VPC CNI branch ENI pods and add ip rules to route probe traffic via veth").Get()
	EnableHostProbeLocalRoute = env.RegisterBoolVar("AMBIENT_ENABLE_HOST_PROBE_LOCAL_ROUTE", false,
		"If true, install local host probe SNAT routes for CNIs that return local destinations to host netfilter").Get()
)

const (
	// to reliably identify kubelet healthprobes from inside the pod (versus standard kube-proxy traffic,
	// since the IP is normally the same), we SNAT identified host probes in the host netns to a fixed
	// APIPA/"link-local" IP.
	//
	// It doesn't matter what this IP is, so long as it's not routable and doesn't collide with anything else.
	//
	// The IPv6 marker is a unique-local address, not an IPv6 link-local address.
	DefaultHostProbeSNATIP   = "169.254.7.127"
	DefaultHostProbeSNATIPV6 = "fd16:9254:7127:1337:ffff:ffff:ffff:ffff"
)

type AmbientArgs struct {
	CNIAgentRunDir             string
	SystemNamespace            string
	Revision                   string
	KubeConfig                 string
	ServerSocket               string
	EnablementSelector         *util.CompiledEnablementSelectors
	ExcludeNamespaces          []string
	DNSCapture                 bool
	EnableIPv6                 bool
	ReconcilePodRulesOnStartup bool
	NativeNftables             bool
	ForceIptablesBinary        string
}
