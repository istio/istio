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

// The auth package provides support for checking the authentication and authorization policy applied
// in the mesh. It aims to increase the debuggability and observability of auth policies.
// Note: this is still under active development and is not ready for real use.
package auth

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"

	envoy_admin_v2alpha "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"

	"istio.io/istio/istioctl/pkg/util/configdump"
)

// Analyzer that can be used to check authentication and authorization policy status.
type Analyzer struct {
	nodeIP       string
	nodeType     string
	listenerDump *envoy_admin_v2alpha.ListenersConfigDump
	clusterDump  *envoy_admin_v2alpha.ClustersConfigDump
}

// NewAnalyzer creates a new analyzer for a given pod based on its envoy config.
func NewAnalyzer(envoyConfig *configdump.Wrapper) (*Analyzer, error) {
	bootstrap, err := envoyConfig.GetBootstrapConfigDump()
	if err != nil {
		return nil, fmt.Errorf("failed to get bootstrap config dump: %s", err)
	}
	splits := strings.Split(bootstrap.Bootstrap.Node.Id, "~")
	if len(splits) != 4 {
		return nil, fmt.Errorf("invalid node ID(%q), expecting 4 '~' but found: %d",
			bootstrap.Bootstrap.Node.Id, len(splits))
	}

	listeners, err := envoyConfig.GetDynamicListenerDump(true)
	if err != nil {
		return nil, fmt.Errorf("failed to get dynamic listener dump: %s", err)
	}

	clusters, err := envoyConfig.GetDynamicClusterDump(true)
	if err != nil {
		return nil, fmt.Errorf("failed to get dynamic cluster dump: %s", err)
	}

	return &Analyzer{nodeType: splits[0], nodeIP: splits[1], listenerDump: listeners, clusterDump: clusters}, nil
}

func (a *Analyzer) getParsedListeners() []*ParsedListener {
	ret := make([]*ParsedListener, 0)
	for _, listener := range a.listenerDump.DynamicActiveListeners {
		if listener.Listener.Address.GetSocketAddress().Address == a.nodeIP {
			if ld := ParseListener(listener.Listener); ld != nil {
				ret = append(ret, ld)
			}
		}
	}

	sort.Slice(ret, func(i, j int) bool {
		ipi := net.ParseIP(ret[i].ip)
		ipj := net.ParseIP(ret[j].ip)
		if ipi.Equal(ipj) {
			pi, _ := strconv.Atoi(ret[i].port)
			pj, _ := strconv.Atoi(ret[j].port)
			return pi < pj
		}
		return bytes.Compare(ipi, ipj) < 0
	})
	return ret
}

func (a *Analyzer) getParsedClusters() []*ParsedCluster {
	ret := make([]*ParsedCluster, 0)
	for _, cluster := range a.clusterDump.DynamicActiveClusters {
		if cd := ParseCluster(cluster.Cluster); cd != nil && cd.direction == "outbound" {
			ret = append(ret, cd)
		}
	}

	sort.Slice(ret, func(i, j int) bool {
		if ret[i].domain == ret[j].domain {
			if ret[i].ns == ret[j].ns {
				if ret[i].short == ret[j].short {
					pi, _ := strconv.Atoi(ret[i].port)
					pj, _ := strconv.Atoi(ret[j].port)
					return pi < pj
				}
				return ret[i].short < ret[j].short
			}
			return ret[i].ns < ret[j].ns
		}
		return ret[i].domain < ret[j].domain
	})
	return ret
}

// PrintTLS checks the TLS/JWT/RBAC setting for the given envoy config stored in the analyzer.
func (a *Analyzer) PrintTLS(writer io.Writer, printAll bool) {
	parsedListeners := a.getParsedListeners()
	_, _ = fmt.Fprintf(writer, "Checked %d/%d listeners with node IP %s.\n",
		len(parsedListeners), len(a.listenerDump.DynamicActiveListeners), a.nodeIP)
	PrintParsedListeners(writer, parsedListeners, printAll)

	parsedClusters := a.getParsedClusters()
	_, _ = fmt.Fprintf(writer, "\nChecked %d/%d outbound clusters.\n",
		len(parsedClusters), len(a.clusterDump.DynamicActiveClusters))
	PrintParsedClusters(writer, parsedClusters, printAll)
}
