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

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/env"
)

var (
	PodNamespace    = env.RegisterStringVar("SYSTEM_NAMESPACE", constants.IstioSystemNamespace, "pod's namespace").Get()
	PodName         = env.RegisterStringVar("POD_NAME", "", "").Get()
	NodeName        = env.RegisterStringVar("NODE_NAME", "", "").Get()
	Revision        = env.RegisterStringVar("REVISION", "", "").Get()
	HostProbeSNATIP = netip.MustParseAddr(env.RegisterStringVar("HOST_PROBE_SNAT_IP", DefaultHostProbeSNATIP, "").Get())
)

const (
	// to reliably identify kubelet healthprobes from inside the pod (versus standard kube-proxy traffic,
	// since the IP is normally the same), we SNAT identified host probes in the host netns to a fixed APIPA IP.
	//
	// It doesn't matter what this IP is, so long as it's not routable and doesn't collide with anything else.
	// (Can be ipv6)
	DefaultHostProbeSNATIP = "169.254.7.127"
)

type AmbientArgs struct {
	SystemNamespace string
	Revision        string
	KubeConfig      string
	ServerSocket    string
	DNSCapture      bool
}
