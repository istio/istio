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

package krt_test

import (
	"fmt"
	"net/netip"
	"strings"

	corev1 "k8s.io/api/core/v1"

	istio "istio.io/api/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/serviceentry"
	srkube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/ambient"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/workloadapi"
)

func serviceBuilder() krt.TransformationSingle[*corev1.Service, model.ServiceInfo] {
	return func(ctx krt.HandlerContext, s *corev1.Service) *model.ServiceInfo {
		if s.Spec.Type == corev1.ServiceTypeExternalName {
			// ExternalName services are not implemented by ambient (but will still work).
			// The DNS requests will forward to the upstream DNS server, then Ztunnel can handle the request based on the target
			// hostname.
			// In theory we could add support for native 'DNS alias' into Ztunnel's DNS proxy. This would give the same behavior
			// but let the DNS proxy handle it instead of forwarding upstream. However, at this time we do not do so.
			return nil
		}
		portNames := map[int32]model.ServicePortName{}
		for _, p := range s.Spec.Ports {
			portNames[p.Port] = model.ServicePortName{
				PortName:       p.Name,
				TargetPortName: p.TargetPort.StrVal,
			}
		}
		svc := constructService(ctx, s)
		return &model.ServiceInfo{
			Service:       svc,
			PortNames:     portNames,
			LabelSelector: model.NewSelector(s.Spec.Selector),
			Source:        ambient.MakeSource(s),
		}
	}
}

func serviceEntriesBuilder() krt.TransformationMulti[*istioclient.ServiceEntry, model.ServiceInfo] {
	return func(ctx krt.HandlerContext, se *istioclient.ServiceEntry) []model.ServiceInfo {
		sel := model.NewSelector(se.Spec.GetWorkloadSelector().GetLabels())
		portNames := map[int32]model.ServicePortName{}
		for _, p := range se.Spec.Ports {
			portNames[int32(p.Number)] = model.ServicePortName{
				PortName: p.Name,
			}
		}
		return slices.Map(constructServiceEntries(ctx, se), func(e *workloadapi.Service) model.ServiceInfo {
			return model.ServiceInfo{
				Service:       e,
				PortNames:     portNames,
				LabelSelector: sel,
				Source:        ambient.MakeSource(se),
			}
		})
	}
}

// This function has a copy of some functionality from the ambient controller so that we
// can test that some of the krt functionality works the way we expect

func constructService(_ krt.HandlerContext, svc *corev1.Service) *workloadapi.Service {
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, &workloadapi.Port{
			ServicePort: uint32(p.Port),
			TargetPort:  uint32(p.TargetPort.IntVal),
		})
	}

	addresses, err := slices.MapErr(getVIPs(svc), func(vip string) (*workloadapi.NetworkAddress, error) {
		ip, err := netip.ParseAddr(vip)
		if err != nil {
			return nil, fmt.Errorf("parse %v: %v", vip, err)
		}
		return &workloadapi.NetworkAddress{
			Network: "testnetwork",
			Address: ip.AsSlice(),
		}, nil
	})
	if err != nil {
		log.Warnf("fail to parse service %v: %v", config.NamespacedName(svc), err)
		return nil
	}

	// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
	return &workloadapi.Service{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Hostname:  string(srkube.ServiceHostname(svc.Name, svc.Namespace, "test.local")),
		Addresses: addresses,
		Ports:     ports,
	}
}

func constructServiceEntries(_ krt.HandlerContext, svc *istioclient.ServiceEntry) []*workloadapi.Service {
	var autoassignedHostAddresses map[string][]netip.Addr
	addresses, err := slices.MapErr(svc.Spec.Addresses, func(vip string) (*workloadapi.NetworkAddress, error) {
		ip, err := parseCidrOrIP(vip)
		if err != nil {
			return nil, err
		}
		return &workloadapi.NetworkAddress{
			Network: "testnetwork",
			Address: ip.AsSlice(),
		}, nil
	})
	if err != nil {
		return nil
	}
	// if this se has autoallocation we can se autoallocated IP, otherwise it will remain an empty slice
	if serviceentry.ShouldV2AutoAllocateIP(svc) {
		autoassignedHostAddresses = serviceentry.GetHostAddressesFromServiceEntry(svc)
	}
	ports := make([]*workloadapi.Port, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		target := p.TargetPort
		if target == 0 {
			target = p.Number
		}
		ports = append(ports, &workloadapi.Port{
			ServicePort: p.Number,
			TargetPort:  target,
		})
	}

	lb := &workloadapi.LoadBalancing{}

	res := make([]*workloadapi.Service, 0, len(svc.Spec.Hosts))
	for _, h := range svc.Spec.Hosts {
		// if we have no user-provided hostsAddresses and h is not wildcarded and we have hostsAddresses supported resolution
		// we can try to use autoassigned hostsAddresses
		hostsAddresses := addresses
		if len(hostsAddresses) == 0 && !host.Name(h).IsWildCarded() && svc.Spec.Resolution != istio.ServiceEntry_NONE {
			if hostsAddrs, ok := autoassignedHostAddresses[h]; ok {
				hostsAddresses = slices.Map(hostsAddrs, func(ip netip.Addr) *workloadapi.NetworkAddress {
					return &workloadapi.NetworkAddress{
						Network: "testnetwork",
						Address: ip.AsSlice(),
					}
				})
			}
		}
		res = append(res, &workloadapi.Service{
			Name:            svc.Name,
			Namespace:       svc.Namespace,
			Hostname:        h,
			Addresses:       hostsAddresses,
			Ports:           ports,
			SubjectAltNames: svc.Spec.SubjectAltNames,
			LoadBalancing:   lb,
		})
	}
	return res
}

func getVIPs(svc *corev1.Service) []string {
	res := []string{}
	cips := svc.Spec.ClusterIPs
	if len(cips) == 0 {
		cips = []string{svc.Spec.ClusterIP}
	}
	for _, cip := range cips {
		if cip != "" && cip != corev1.ClusterIPNone {
			res = append(res, cip)
		}
	}
	return res
}

func networkAddressFromService(s model.ServiceInfo) []networkAddress {
	networkAddrs := make([]networkAddress, 0, len(s.Service.Addresses))
	for _, addr := range s.Service.Addresses {
		// mustByteIPToString is ok since this is from our IP constructed
		networkAddrs = append(networkAddrs, networkAddress{network: addr.Network, ip: mustByteIPToString(addr.Address)})
	}
	return networkAddrs
}

type networkAddress struct {
	network string
	ip      string
}

func (n networkAddress) String() string {
	return n.network + "/" + n.ip
}

func parseCidrOrIP(ip string) (netip.Addr, error) {
	if strings.Contains(ip, "/") {
		prefix, err := netip.ParsePrefix(ip)
		if err != nil {
			return netip.Addr{}, err
		}
		if !prefix.IsSingleIP() {
			return netip.Addr{}, fmt.Errorf("only single IP CIDR is allowed")
		}
		return prefix.Addr(), nil
	}
	return netip.ParseAddr(ip)
}

func mustByteIPToString(b []byte) string {
	ip, _ := netip.AddrFromSlice(b) // Address only comes from objects we create, so it must be valid
	return ip.String()
}
