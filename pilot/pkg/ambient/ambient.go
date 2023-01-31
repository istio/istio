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
	"time"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/spiffe"
)

type WorkloadMetadata struct {
	Containers     []string
	GenerateName   string
	ControllerName string
	ControllerKind string
}

// TODO shouldn't call this a workload. Maybe "node" encapsulates all of ztunnel, waypoints, workload
type Workload struct {
	UID         string
	Name        string
	Namespace   string
	Annotations map[string]string
	Labels      map[string]string

	ServiceAccount string
	NodeName       string
	PodIP          string
	PodIPs         []string
	HostNetwork    bool

	WorkloadMetadata

	CreationTimestamp time.Time
}

// Identity generates SecureNamingSAN but for Workload instead of Pod
func (w Workload) Identity() string {
	return spiffe.MustGenSpiffeURI(w.Namespace, w.ServiceAccount)
}

func (w Workload) Equals(w2 Workload) bool {
	if w.UID != w2.UID {
		return false
	}
	if w.Name != w2.Name {
		return false
	}
	if w.Namespace != w2.Namespace {
		return false
	}

	if w.ServiceAccount != w2.ServiceAccount {
		return false
	}
	if w.NodeName != w2.NodeName {
		return false
	}
	if w.PodIP != w2.PodIP {
		return false
	}
	if !w.CreationTimestamp.Equal(w2.CreationTimestamp) {
		return false
	}
	if !maps.Equal(w.Labels, w2.Labels) {
		return false
	}
	if !maps.Equal(w.Annotations, w2.Annotations) {
		return false
	}
	if !slices.Equal(w.PodIPs, w2.PodIPs) {
		return false
	}
	if !slices.Equal(w.WorkloadMetadata.Containers, w2.WorkloadMetadata.Containers) {
		return false
	}
	if w.WorkloadMetadata.GenerateName != w2.WorkloadMetadata.GenerateName {
		return false
	}
	if w.WorkloadMetadata.ControllerName != w2.WorkloadMetadata.ControllerName {
		return false
	}
	if w.WorkloadMetadata.ControllerKind != w2.WorkloadMetadata.ControllerKind {
		return false
	}

	return true
}

type NodeType = string

const (
	LabelStatus = "istio.io/ambient-status"
	TypeEnabled = "enabled"
	// LabelType == "workload" -> intercept into ztunnel
	// TODO this could be an annotation – eventually move it into api repo
	LabelType = "ambient-type"

	TypeWorkload NodeType = "workload"
	TypeNone     NodeType = "none"
	TypeZTunnel  NodeType = "ztunnel"
	TypeWaypoint NodeType = "waypoint"
)

// Cache holds Indexes of client workloads, waypoint proxies, and ztunnels
type Cache interface {
	AmbientWorkloads() Indexes
}

type Indexes struct {
	Workloads *WorkloadIndex `json:"workloads"`
	None      *WorkloadIndex `json:"none"`
	Waypoints *WorkloadIndex `json:"waypoints"`
	ZTunnels  *WorkloadIndex `json:"ztunnels"`
}

type WorkloadIndex struct {
	sync.RWMutex

	ByNamespacedName  map[types.NamespacedName]Workload
	ByNode            map[string][]Workload
	ByNamespace       map[string][]Workload
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
		ByNamespace:       map[string][]Workload{},
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
	wi.Lock()
	defer wi.Unlock()
	node, sa := workload.NodeName, workload.Identity()
	ip := workload.PodIP // TODO eventually support multi-IP
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
	wi.ByNamespace[workload.Namespace] = append(wi.ByNamespace[workload.Namespace], workload)
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
	// TODO: this seems wrong, we are deleting the whole {node,identity,etc} when we remove a single workload
	delete(wi.ByNamespacedName, namespacedName)
	delete(wi.ByNamespace, namespacedName.Namespace)
	delete(wi.details, namespacedName)
	delete(wi.ByNode, node)
	delete(wi.ByIdentity, sa)
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
