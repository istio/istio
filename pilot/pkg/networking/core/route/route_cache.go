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

package route

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/hash"
)

var (
	Separator = []byte{'~'}
	Slash     = []byte{'/'}
)

// Cache includes the variables that can influence a Route Configuration.
// Implements XdsCacheEntry interface.
type Cache struct {
	RouteName string

	ProxyVersion string
	// proxy cluster ID
	ClusterID string
	// proxy dns domain
	DNSDomain string
	// DNSCapture indicates whether the workload has enabled dns capture
	DNSCapture bool
	// DNSAutoAllocate indicates whether the workload should have auto allocated addresses for ServiceEntry
	// This allows resolving ServiceEntries, which is especially useful for distinguishing TCP traffic
	// This depends on DNSCapture.
	DNSAutoAllocate bool
	// AllowAny indicates if the proxy should allow all outbound traffic or only known registries
	AllowAny bool

	ListenerPort            int
	Services                []*model.Service
	VirtualServices         []config.Config
	DelegateVirtualServices []model.ConfigHash
	DestinationRules        []*model.ConsolidatedDestRule
	EnvoyFilterKeys         []string
}

func (r *Cache) Type() string {
	return model.RDSType
}

func (r *Cache) Cacheable() bool {
	if r == nil {
		return false
	}
	if r.ListenerPort == 0 {
		return false
	}

	for _, config := range r.VirtualServices {
		vs := config.Spec.(*networking.VirtualService)
		for _, httpRoute := range vs.Http {
			for _, match := range httpRoute.Match {
				// if vs has source match, not cacheable
				if len(match.SourceLabels) > 0 || match.SourceNamespace != "" {
					return false
				}
			}
		}
	}

	return true
}

func extractNamespaceForKubernetesService(hostname string) (string, error) {
	ih := strings.Index(hostname, ".svc.")
	if ih < 0 {
		return "", fmt.Errorf("hostname is a not a Kubernetes name, missing .svc: %v", hostname)
	}
	nsI := strings.Index(hostname, ".")
	if nsI+1 >= len(hostname) || nsI+1 > ih {
		// Invalid domain
		return "", fmt.Errorf("hostname is a not a Kubernetes name, missing namespace: %v", hostname)
	}
	ns := hostname[nsI+1 : ih]
	if len(ns) == 0 {
		return "", fmt.Errorf("namespace not found")
	}
	return ns, nil
}

func (r *Cache) DependentConfigs() []model.ConfigHash {
	size := len(r.Services) + len(r.VirtualServices) + len(r.DelegateVirtualServices) + len(r.EnvoyFilterKeys)
	for _, mergedDR := range r.DestinationRules {
		size += len(mergedDR.GetFrom())
	}
	configs := make([]model.ConfigHash, 0, size)
	for _, svc := range r.Services {
		configs = append(configs, model.ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      string(svc.Hostname),
			Namespace: svc.Attributes.Namespace,
		}.HashCode())
		for _, alias := range svc.Attributes.Aliases {
			configs = append(configs, model.ConfigKey{Kind: kind.ServiceEntry, Name: alias.Hostname.String(), Namespace: alias.Namespace}.HashCode())
		}
	}
	for _, vs := range r.VirtualServices {
		for _, cfg := range model.VirtualServiceDependencies(vs) {
			configs = append(configs, cfg.HashCode())
		}
	}
	// add delegate virtual services to dependent configs
	// so that we can clear the rds cache when delegate virtual services are updated
	configs = append(configs, r.DelegateVirtualServices...)
	for _, mergedDR := range r.DestinationRules {
		for _, dr := range mergedDR.GetFrom() {
			configs = append(configs, model.ConfigKey{Kind: kind.DestinationRule, Name: dr.Name, Namespace: dr.Namespace}.HashCode())
		}
	}

	for _, efKey := range r.EnvoyFilterKeys {
		items := strings.Split(efKey, "/")
		configs = append(configs, model.ConfigKey{Kind: kind.EnvoyFilter, Name: items[1], Namespace: items[0]}.HashCode())
	}
	return configs
}

func (r *Cache) Key() any {
	// nolint: gosec
	// Not security sensitive code
	h := hash.New()

	h.WriteString(r.RouteName)
	h.Write(Separator)
	h.WriteString(r.ProxyVersion)
	h.Write(Separator)
	h.WriteString(r.ClusterID)
	h.Write(Separator)
	h.WriteString(r.DNSDomain)
	h.Write(Separator)
	h.WriteString(strconv.FormatBool(r.DNSCapture))
	h.Write(Separator)
	h.WriteString(strconv.FormatBool(r.DNSAutoAllocate))
	h.Write(Separator)
	h.WriteString(strconv.FormatBool(r.AllowAny))
	h.Write(Separator)

	for _, svc := range r.Services {
		h.WriteString(string(svc.Hostname))
		h.Write(Slash)
		h.WriteString(svc.Attributes.Namespace)
		h.Write(Separator)
	}
	h.Write(Separator)

	for _, vs := range r.VirtualServices {
		for _, cfg := range model.VirtualServiceDependencies(vs) {
			h.WriteString(cfg.Kind.String())
			h.Write(Slash)
			h.WriteString(cfg.Name)
			h.Write(Slash)
			h.WriteString(cfg.Namespace)
			h.Write(Separator)
		}
	}
	h.Write(Separator)

	for _, vs := range r.DelegateVirtualServices {
		h.Write(hashToBytes(vs))
		h.Write(Separator)
	}
	h.Write(Separator)

	for _, mergedDR := range r.DestinationRules {
		for _, dr := range mergedDR.GetFrom() {
			h.WriteString(dr.Name)
			h.Write(Slash)
			h.WriteString(dr.Namespace)
			h.Write(Separator)
		}
	}
	h.Write(Separator)

	for _, efk := range r.EnvoyFilterKeys {
		h.WriteString(efk)
		h.Write(Separator)
	}
	h.Write(Separator)

	return h.Sum64()
}

func hashToBytes(number model.ConfigHash) []byte {
	big := new(big.Int)
	big.SetUint64(uint64(number))
	return big.Bytes()
}
