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
	"crypto/md5"
	"encoding/hex"
	"strconv"
	"strings"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
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

	ListenerPort            int
	Services                []*model.Service
	VirtualServices         []config.Config
	DelegateVirtualServices []model.ConfigKey
	DestinationRules        []*config.Config
	EnvoyFilterKeys         []string
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

func (r *Cache) DependentConfigs() []model.ConfigKey {
	configs := make([]model.ConfigKey, 0, len(r.Services)+len(r.VirtualServices)+
		len(r.DelegateVirtualServices)+len(r.DestinationRules)+len(r.EnvoyFilterKeys))
	for _, svc := range r.Services {
		configs = append(configs, model.ConfigKey{Kind: gvk.ServiceEntry, Name: string(svc.Hostname), Namespace: svc.Attributes.Namespace})
	}
	for _, vs := range r.VirtualServices {
		configs = append(configs, model.ConfigKey{Kind: gvk.VirtualService, Name: vs.Name, Namespace: vs.Namespace})
	}
	// add delegate virtual services to dependent configs
	// so that we can clear the rds cache when delegate virtual services are updated
	configs = append(configs, r.DelegateVirtualServices...)
	for _, dr := range r.DestinationRules {
		configs = append(configs, model.ConfigKey{Kind: gvk.DestinationRule, Name: dr.Name, Namespace: dr.Namespace})
	}

	for _, efKey := range r.EnvoyFilterKeys {
		items := strings.Split(efKey, "/")
		configs = append(configs, model.ConfigKey{Kind: gvk.EnvoyFilter, Name: items[1], Namespace: items[0]})
	}
	return configs
}

func (r *Cache) DependentTypes() []config.GroupVersionKind {
	return nil
}

func (r *Cache) Key() string {
	hash := md5.New()

	hash.Write([]byte(r.RouteName))
	hash.Write(Separator)
	hash.Write([]byte(r.ProxyVersion))
	hash.Write(Separator)
	hash.Write([]byte(r.ClusterID))
	hash.Write(Separator)
	hash.Write([]byte(r.DNSDomain))
	hash.Write(Separator)
	hash.Write([]byte(strconv.FormatBool(r.DNSCapture)))
	hash.Write(Separator)
	hash.Write([]byte(strconv.FormatBool(r.DNSAutoAllocate)))
	hash.Write(Separator)

	for _, svc := range r.Services {
		hash.Write([]byte(svc.Hostname))
		hash.Write(Slash)
		hash.Write([]byte(svc.Attributes.Namespace))
		hash.Write(Separator)
	}
	hash.Write(Separator)

	for _, vs := range r.VirtualServices {
		hash.Write([]byte(vs.Name))
		hash.Write(Slash)
		hash.Write([]byte(vs.Namespace))
		hash.Write(Separator)
	}
	hash.Write(Separator)

	for _, vs := range r.DelegateVirtualServices {
		hash.Write([]byte(vs.Name))
		hash.Write(Slash)
		hash.Write([]byte(vs.Namespace))
		hash.Write(Separator)
	}
	hash.Write(Separator)

	for _, dr := range r.DestinationRules {
		hash.Write([]byte(dr.Name))
		hash.Write(Slash)
		hash.Write([]byte(dr.Namespace))
		hash.Write(Separator)
	}
	hash.Write(Separator)

	for _, efk := range r.EnvoyFilterKeys {
		hash.Write([]byte(efk))
		hash.Write(Separator)
	}
	hash.Write(Separator)

	sum := hash.Sum(nil)
	return hex.EncodeToString(sum)
}
