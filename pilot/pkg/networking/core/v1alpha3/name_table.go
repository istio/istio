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

package v1alpha3

import (
	"net"

	"istio.io/istio/pilot/pkg/model"
	nds "istio.io/istio/pilot/pkg/proto"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
)

// BuildNameTable produces a table of hostnames and their associated IPs that can then
// be used by the agent to resolve DNS. This logic is always active. However, local DNS resolution
// will only be effective if DNS capture is enabled in the proxy
func (configgen *ConfigGeneratorImpl) BuildNameTable(node *model.Proxy, push *model.PushContext) *nds.NameTable {
	if node.Type != model.SidecarProxy {
		// DNS resolution is only for sidecars
		return nil
	}

	out := &nds.NameTable{
		Table: map[string]*nds.NameTable_NameInfo{},
	}

	for _, svc := range push.Services(node) {
		// we cannot take services with wildcards in the address field. The reason
		// is that even if we provide some dummy IP (subject to enabling this
		// feature in Envoy), after capturing the traffic from the app, the
		// sidecar would need to forward to the real IP. But to determine the real
		// IP, the sidecar would have to know the non-wildcard FQDN that the
		// application was trying to resolve. This information is not available
		// for TCP services. The wildcard hostname is not a problem for HTTP
		// services though, as we usually setup a listener on 0.0.0.0, process
		// based on http virtual host and forward to the orig destination IP.
		//
		// Long story short, if the user has a TCP service of the form
		//
		// host: *.mysql.aws.com, port 3306,
		//
		// our only recourse is to allocate a 0.0.0.0:3306 passthrough listener and forward to
		// original dest IP. It is now the user's responsibility to not allocate
		// another wildcard service on the same port. i.e.
		//
		// 1. host: *.mysql.aws.com, port 3306
		// 2. host: *.mongo.aws.com, port 3306 will result in conflict.
		//
		// Traffic will still flow but metrics wont be correct
		// as two different TCP services are consuming the
		// same wildcard passthrough TCP listener 0.0.0.0:3306.
		//
		if svc.Hostname.IsWildCarded() {
			continue
		}

		svcAddress := svc.GetServiceAddressForProxy(node, push)
		var addressList []string

		// The IP will be unspecified here if its headless service or if the auto
		// IP allocation logic for service entry was unable to allocate an IP.
		if svcAddress == constants.UnspecifiedIP {
			// For all k8s headless services, populate the dns table with the endpoint IPs as k8s does.
			// TODO: Need to have an entry per pod hostname of stateful set but for this, we need to parse
			// the stateful set object, associate the object with the appropriate kubernetes headless service
			// and then derive the stable network identities.
			if svc.Attributes.ServiceRegistry == string(serviceregistry.Kubernetes) &&
				svc.Resolution == model.Passthrough && len(svc.Ports) > 0 {
				// TODO: this is used in two places now. Needs to be cached as part of the headless service
				// object to avoid the costly lookup in the registry code
				for _, instance := range push.ServiceInstancesByPort(svc, svc.Ports[0].Port, nil) {
					// TODO: should we skip the node's own IP like we do in listener?
					addressList = append(addressList, instance.Endpoint.Address)
				}
			}

			if len(addressList) == 0 {
				// could not reliably determine the addresses of endpoints of headless service
				// or this is not a k8s service
				continue
			}
		} else {
			// Filter out things we cannot parse as IP. Generally this means CIDRs, as anything else
			// should be caught in validation.
			if addr := net.ParseIP(svcAddress); addr == nil {
				continue
			}
			addressList = append(addressList, svcAddress)
		}

		nameInfo := &nds.NameTable_NameInfo{
			Ips:      addressList,
			Registry: svc.Attributes.ServiceRegistry,
		}
		if svc.Attributes.ServiceRegistry == string(serviceregistry.Kubernetes) {
			// The agent will take care of resolving a, a.ns, a.ns.svc, etc.
			// No need to provide a DNS entry for each variant.
			nameInfo.Namespace = svc.Attributes.Namespace
			nameInfo.Shortname = svc.Attributes.Name
		}
		out.Table[string(svc.Hostname)] = nameInfo
	}
	return out
}
