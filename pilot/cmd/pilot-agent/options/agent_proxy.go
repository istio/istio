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

package options

import (
	"strings"

	"istio.io/istio/pkg/model"
)

// ProxyArgs provides all of the configuration parameters for the Pilot proxy.
type ProxyArgs struct {
	DNSDomain          string
	StsPort            int
	TokenManagerPlugin string

	MeshConfigFile string

	// proxy config flags (named identically)
	ServiceCluster         string
	ProxyLogLevel          string
	ProxyComponentLogLevel string
	Concurrency            int
	TemplateFile           string
	OutlierLogPath         string

	PodName      string
	PodNamespace string

	// enableProfiling enables profiling via web interface host:port/debug/pprof/
	EnableProfiling bool

	// Shared properties with Pilot Proxy struct.
	ID          string
	IPAddresses []string
	Type        model.NodeType
	ipMode      model.IPMode
}

// NewProxyArgs constructs proxyArgs with default values.
func NewProxyArgs() ProxyArgs {
	p := ProxyArgs{}

	// Apply Default Values.
	p.applyDefaults()

	return p
}

// applyDefaults apply default value to ProxyArgs
func (node *ProxyArgs) applyDefaults() {
	node.PodName = PodNameVar.Get()
	node.PodNamespace = PodNamespaceVar.Get()
}

func (node *ProxyArgs) DiscoverIPMode() {
	node.ipMode = model.DiscoverIPMode(node.IPAddresses)
}

// IsIPv6 returns true if proxy only supports IPv6 addresses.
func (node *ProxyArgs) IsIPv6() bool {
	return node.ipMode == model.IPv6
}

func (node *ProxyArgs) SupportsIPv6() bool {
	return node.ipMode == model.IPv6 || node.ipMode == model.Dual
}

const (
	serviceNodeSeparator = "~"
)

func (node *ProxyArgs) ServiceNode() string {
	ip := ""
	if len(node.IPAddresses) > 0 {
		ip = node.IPAddresses[0]
	}
	return strings.Join([]string{
		string(node.Type), ip, node.ID, node.DNSDomain,
	}, serviceNodeSeparator)
}
