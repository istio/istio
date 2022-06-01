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
	"encoding/json"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/spiffe"
)

// TODO shouldn't call this a workload. Maybe "node" encapsulates all of uproxy, pep, workload
type Workload struct {
	*corev1.Pod // TODO extract important things instead of embedding Pod
}

func (w Workload) Identity() string {
	// TODO duplicated to avoid import cycle
	return spiffe.MustGenSpiffeURI(w.Pod.Namespace, w.Pod.Spec.ServiceAccountName)
}

type NodeType = string

const (
	// LabelType == "workload" -> intercept into uProxy
	// TODO this could be an annotation – eventually move it into api repo
	LabelType = "ambient-type"
	// Needed for bpf code currently
	LegacyLabelType = "asm-type"

	TypeWorkload NodeType = "workload"
	TypeNone     NodeType = "none"
	TypeUProxy   NodeType = "uproxy"
	TypePEP      NodeType = "pep"

	// LabelProxy contains the name of the service account the proxy is a PEP for.
	LabelProxy = "ambient-proxy"
	// LabelPEP marks the Pod as needing a PEP (remote proxy/policy enforcement point)
	LabelPEP = "ambient-pep"

	TransportMatchKey   = "transport"
	TransportMatchValue = "tunnel"
)

// Cache holds Indexes of client workloads, peps and uproxies
type Cache interface {
	SidecarlessWorkloads() Indexes
}

type Indexes struct {
	Workloads *WorkloadIndex `json:"workloads"`
	None      *WorkloadIndex `json:"none"`
	PEPs      *WorkloadIndex `json:"peps"`
	UProxies  *WorkloadIndex `json:"uproxies"`
}

type WorkloadIndex struct {
	sync.RWMutex

	ByNamespacedName  map[types.NamespacedName]Workload
	ByNode            map[string][]Workload
	ByIdentity        map[string][]Workload
	ByNodeAndIdentity map[string]map[string][]Workload
	ByIP              map[string]Workload

	// we cache this so we can cleanup the other indexes on removal without searching
	details map[types.NamespacedName]subindexDetails
}

func (wi *WorkloadIndex) MarshalJSON() ([]byte, error) {
	return json.Marshal(wi.All())
}

type subindexDetails struct {
	sa, node, ip string
}

func NewWorkloadIndex() *WorkloadIndex {
	// TODO take opts that disable individual indexes if needed
	return &WorkloadIndex{
		ByNamespacedName:  map[types.NamespacedName]Workload{},
		ByNode:            map[string][]Workload{},
		ByIdentity:        map[string][]Workload{},
		ByNodeAndIdentity: map[string]map[string][]Workload{},
		ByIP:              map[string]Workload{},
		details:           map[types.NamespacedName]subindexDetails{},
	}
}

func (wi *WorkloadIndex) NodeLocalBySA(node string) map[string][]Workload {
	if node == "" {
		return wi.ByIdentity
	}
	out := wi.ByNodeAndIdentity[node]
	if out == nil {
		out = map[string][]Workload{}
	}
	return out
}

func (wi *WorkloadIndex) NodeLocal(node string) []Workload {
	if node == "" {
		return wi.All()
	}
	return wi.ByNode[node]
}

func (wi *WorkloadIndex) All() []Workload {
	var out []Workload
	for _, workload := range wi.ByNamespacedName {
		out = append(out, workload)
	}
	return out
}

func (wi *WorkloadIndex) Insert(workload Workload) {
	if workload.Pod == nil {
		return
	}
	wi.Lock()
	defer wi.Unlock()
	node, sa := workload.Spec.NodeName, workload.Identity()
	ip := workload.Status.PodIP // TODO eventually support multi-IP
	namespacedName := types.NamespacedName{Name: workload.Name, Namespace: workload.Namespace}

	// TODO if we start indexing by a mutable key, call Remove here

	wi.ByNamespacedName[namespacedName] = workload
	wi.details[namespacedName] = subindexDetails{node: node, sa: sa, ip: ip}
	if node != "" {
		wi.ByNode[node] = append(wi.ByNode[node], workload)
	}
	if sa != "" {
		wi.ByIdentity[sa] = append(wi.ByIdentity[sa], workload)
	}
	if node != "" && sa != "" {
		if wi.ByNodeAndIdentity[node] == nil {
			wi.ByNodeAndIdentity[node] = map[string][]Workload{}
		}
		if sa != "" {
			wi.ByNodeAndIdentity[node][sa] = append(wi.ByNodeAndIdentity[node][sa], workload)
		}
	}
	if ip != "" {
		wi.ByIP[ip] = workload
	}
}

func (wi *WorkloadIndex) MergeInto(other *WorkloadIndex) *WorkloadIndex {
	wi.RLock()
	defer wi.RUnlock()
	for _, v := range wi.ByNamespacedName {
		other.Insert(v)
	}
	return other
}

func (wi *WorkloadIndex) Remove(namespacedName types.NamespacedName) {
	wi.Lock()
	defer wi.Unlock()
	details, ok := wi.details[namespacedName]
	if !ok {
		return
	}
	node, sa, ip := details.node, details.sa, details.ip
	delete(wi.ByNamespacedName, namespacedName)
	delete(wi.details, namespacedName)
	delete(wi.ByNode, node)
	delete(wi.ByNode, sa)
	delete(wi.ByIP, ip)
	if bySA, ok := wi.ByNodeAndIdentity[node]; ok {
		delete(bySA, sa)
	}
	if len(wi.ByNodeAndIdentity[node]) == 0 {
		delete(wi.ByNodeAndIdentity, node)
	}
}

func (wi *WorkloadIndex) Copy() *WorkloadIndex {
	return wi.MergeInto(NewWorkloadIndex())
}
