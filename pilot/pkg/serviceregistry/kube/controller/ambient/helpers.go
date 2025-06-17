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

package ambient

import (
	"fmt"
	"net/netip"
	"strings"

	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/workloadapi"
)

func generatePodUID(clusterID cluster.ID, p *v1.Pod) string {
	return clusterID.String() + "//" + "Pod/" + p.Namespace + "/" + p.Name
}

func generateWorkloadEntryUID(clusterID cluster.ID, wkEntryNamespace, wkEntryName string) string {
	return clusterID.String() + "/networking.istio.io/WorkloadEntry/" + wkEntryNamespace + "/" + wkEntryName
}

// UID for split-horizon EDS workload that represents all the remote workloads of a service in another network.
func generateEDSWorkloadUID(networkID network.ID, remoteWorkloadNamespace, remoteServiceName string) string {
	return networkID.String() + "/SplitHorizonEDSWorkload/" + remoteWorkloadNamespace + "/" + remoteServiceName
}

func generateServiceEntryUID(clusterID cluster.ID, svcEntryNamespace, svcEntryName, addr string) string {
	return clusterID.String() + "/networking.istio.io/ServiceEntry/" + svcEntryNamespace + "/" + svcEntryName + "/" + addr
}

func workloadToAddress(w *workloadapi.Workload) *workloadapi.Address {
	return &workloadapi.Address{
		Type: &workloadapi.Address_Workload{
			Workload: w,
		},
	}
}

func modelWorkloadToAddressInfo(w model.WorkloadInfo) model.AddressInfo {
	return w.AsAddress
}

func serviceToAddress(s *workloadapi.Service) *workloadapi.Address {
	return &workloadapi.Address{
		Type: &workloadapi.Address_Service{
			Service: s,
		},
	}
}

func mustByteIPToString(b []byte) string {
	ip, _ := netip.AddrFromSlice(b) // Address only comes from objects we create, so it must be valid
	return ip.String()
}

func toNetworkAddress(ctx krt.HandlerContext, vip string, networkGetter func(krt.HandlerContext) network.ID) (*workloadapi.NetworkAddress, error) {
	ip, err := netip.ParseAddr(vip)
	if err != nil {
		return nil, fmt.Errorf("parse %v: %v", vip, err)
	}
	return &workloadapi.NetworkAddress{
		Network: networkGetter(ctx).String(),
		Address: ip.AsSlice(),
	}, nil
}

func toNetworkAddressFromIP(ip netip.Addr, netw network.ID) *workloadapi.NetworkAddress {
	return &workloadapi.NetworkAddress{
		Network: netw.String(),
		Address: ip.AsSlice(),
	}
}

func toNetworkAddressFromCidr(vip string, nw network.ID) (*workloadapi.NetworkAddress, error) {
	ip, err := parseCidrOrIP(vip)
	if err != nil {
		return nil, err
	}
	return &workloadapi.NetworkAddress{
		Network: nw.String(),
		Address: ip.AsSlice(),
	}, nil
}

// parseCidrOrIP parses an IP or a CIDR of a exactly 1 IP (e.g. /32).
// This is to support ServiceEntry which supports CIDRs, but we don't currently support more than 1 IP
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

func AppendNonNil[T any](data []T, i *T) []T {
	if i != nil {
		data = append(data, *i)
	}
	return data
}

// IsPodReady is copied from kubernetes/pkg/api/v1/pod/utils.go
func IsPodReady(pod *v1.Pod) bool {
	return IsPodReadyConditionTrue(pod.Status)
}

// IsPodReadyConditionTrue returns true if a pod is ready; false otherwise.
func IsPodReadyConditionTrue(status v1.PodStatus) bool {
	condition := GetPodReadyCondition(status)
	return condition != nil && condition.Status == v1.ConditionTrue
}

func GetPodReadyCondition(status v1.PodStatus) *v1.PodCondition {
	_, condition := GetPodCondition(&status, v1.PodReady)
	return condition
}

func GetPodCondition(status *v1.PodStatus, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if status == nil {
		return -1, nil
	}
	return GetPodConditionFromList(status.Conditions, conditionType)
}

// GetPodConditionFromList extracts the provided condition from the given list of condition and
// returns the index of the condition and the condition. Returns -1 and nil if the condition is not present.
func GetPodConditionFromList(conditions []v1.PodCondition, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if conditions == nil {
		return -1, nil
	}
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return i, &conditions[i]
		}
	}
	return -1, nil
}

func FindPortName(pod *v1.Pod, name string) (int32, bool) {
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.Name == name && port.Protocol == v1.ProtocolTCP {
				return port.ContainerPort, true
			}
		}
	}
	return 0, false
}

func namespacedHostname(namespace, hostname string) string {
	return namespace + "/" + hostname
}

func networkAddressFromWorkload(wl model.WorkloadInfo) []networkAddress {
	networkAddrs := make([]networkAddress, 0, len(wl.Workload.Addresses))
	for _, addr := range wl.Workload.Addresses {
		// mustByteIPToString is ok since this is from our IP constructed
		networkAddrs = append(networkAddrs, networkAddress{network: wl.Workload.Network, ip: mustByteIPToString(addr)})
	}
	return networkAddrs
}

func networkAddressFromService(s model.ServiceInfo) []networkAddress {
	networkAddrs := make([]networkAddress, 0, len(s.Service.Addresses))
	for _, addr := range s.Service.Addresses {
		// mustByteIPToString is ok since this is from our IP constructed
		networkAddrs = append(networkAddrs, networkAddress{network: addr.Network, ip: mustByteIPToString(addr.Address)})
	}
	return networkAddrs
}

// internal object used for indexing in ambientindex maps
type networkAddress struct {
	network string
	ip      string
}

func (n networkAddress) String() string {
	return n.network + "/" + n.ip
}
